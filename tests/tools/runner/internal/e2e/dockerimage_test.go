package e2e

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeProbeContext(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for path, body := range map[string]string{
		".dockerignore":                     "# comment\ntests/\nnode_modules/\ndocs/\n!docs/examples/\n*.md\n",
		"Dockerfile":                        "FROM alpine\nCOPY . .\n",
		"main.go":                           "package main\n",
		"scripts/build.sh":                  "echo build\n",
		"tests/e2e/scenarios/probe.sh":      "echo scenario\n",
		"tests/tools/docker/cft.Dockerfile": "FROM ubuntu\nARG VERSION=1\n",
		"docs/examples/enrich/main.go":      "package main\n",
		"node_modules/dep/index.js":         "module.exports = 1\n",
	} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

var probeSpec = smokeImageSpec{Repo: "probe-image", Dockerfile: "Dockerfile"}

var probeCFTSpec = smokeImageSpec{
	Repo:       "probe-cft",
	Dockerfile: "tests/tools/docker/cft.Dockerfile",
	Platform:   "linux/amd64",
}

func digestOf(t *testing.T, root string, spec smokeImageSpec) string {
	t.Helper()
	digest, err := buildInputsDigest(root, spec)
	if err != nil {
		t.Fatalf("buildInputsDigest: %v", err)
	}
	if digest == "" {
		t.Fatal("empty digest")
	}
	return digest
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// "Unchanged" has to mean the bytes are the same, not that nothing was touched. A
// checkout, a stash pop or a rebuild of a generated file all move mtimes without changing
// what docker would put in the image, and an mtime-keyed cache rebuilds on every one of
// them — which is the cost this card exists to remove.
func TestBuildInputDigestTracksContentNotTimestamps(t *testing.T) {
	root := writeProbeContext(t)
	before := digestOf(t, root, probeSpec)

	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(filepath.Join(root, "main.go"), future, future); err != nil {
		t.Fatal(err)
	}
	if after := digestOf(t, root, probeSpec); after != before {
		t.Errorf("touching a file changed the digest (%s -> %s); the lane would rebuild for a file whose content is identical", before, after)
	}

	writeFile(t, root, "main.go", "package main // edited\n")
	edited := digestOf(t, root, probeSpec)
	if edited == before {
		t.Fatal("editing a context file did not change the digest; a stale image would be reused for changed inputs")
	}

	writeFile(t, root, "main.go", "package main\n")
	if restored := digestOf(t, root, probeSpec); restored != before {
		t.Errorf("restoring the original bytes did not restore the digest (%s != %s)", restored, before)
	}
}

// The digest must cover the Dockerfile itself even when it lives somewhere the context
// walk never reaches — `docker build -f` happily points outside the context, and the CFT
// Dockerfile sits under tests/, which .dockerignore excludes.
func TestBuildInputDigestCoversADockerfileOutsideTheContext(t *testing.T) {
	root := writeProbeContext(t)
	before := digestOf(t, root, probeCFTSpec)

	writeFile(t, root, "tests/tools/docker/cft.Dockerfile", "FROM ubuntu\nARG VERSION=2\n")
	if after := digestOf(t, root, probeCFTSpec); after == before {
		t.Fatal("editing the Dockerfile did not change the digest, so a changed Dockerfile would silently reuse the image built from the old one")
	}
}

func TestBuildInputDigestSeparatesImagesBuiltFromTheSameContext(t *testing.T) {
	root := writeProbeContext(t)
	if digestOf(t, root, probeSpec) == digestOf(t, root, probeCFTSpec) {
		t.Error("two images with different Dockerfiles and platforms share a digest, so one would reuse the other's image")
	}
}

// A file docker never sends must not force a rebuild — that is the whole saving, since
// the smoke scenarios under tests/ are the files that change most often. Everything the
// .dockerignore parser cannot decide exactly stays IN the digest, because over-rebuilding
// is loud and cheap while under-rebuilding is a stale image passing a run it should fail.
func TestBuildInputDigestFollowsTheDockerignoreOnlyWhereItIsUnambiguous(t *testing.T) {
	root := writeProbeContext(t)
	before := digestOf(t, root, probeSpec)

	for _, tc := range []struct {
		name       string
		file       string
		body       string
		wantChange bool
	}{
		{
			name: "a plainly excluded directory does not affect the digest",
			file: "tests/e2e/scenarios/probe.sh", body: "echo edited\n", wantChange: false,
		},
		{
			name: "an excluded directory nested anywhere does not affect the digest",
			file: "node_modules/dep/index.js", body: "module.exports = 2\n", wantChange: false,
		},
		{
			name: "a directory with a negation stays in the digest, because the negation puts part of it back",
			file: "docs/examples/enrich/main.go", body: "package main // edited\n", wantChange: true,
		},
		{
			name: "the .dockerignore itself is an input: changing what is excluded changes the image",
			file: ".dockerignore", body: "# comment\nnode_modules/\ndocs/\n!docs/examples/\n*.md\n", wantChange: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := writeProbeContext(t)
			writeFile(t, root, tc.file, tc.body)
			changed := digestOf(t, root, probeSpec) != before
			if changed != tc.wantChange {
				t.Errorf("editing %s changed the digest = %v, want %v", tc.file, changed, tc.wantChange)
			}
		})
	}
}

func probeRunner(t *testing.T, root string, args Args) *Runner {
	t.Helper()
	return &Runner{args: args, repoRoot: root, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
}

func TestSmokeImageIsReusedOnlyWhenAnImageForTheseInputsExists(t *testing.T) {
	root := writeProbeContext(t)
	digest := digestOf(t, root, probeSpec)
	present := func(ref string) bool { return ref == "probe-image:"+digest }
	absent := func(string) bool { return false }

	reused := probeRunner(t, root, Args{}).resolveSmokeImage(probeSpec, present)
	if reused.Build {
		t.Errorf("rebuilt an image whose inputs are unchanged: %s", reused.Reason)
	}
	if reused.Ref() != "probe-image:"+digest {
		t.Errorf("ref = %q, want the digest-tagged image", reused.Ref())
	}

	missing := probeRunner(t, root, Args{}).resolveSmokeImage(probeSpec, absent)
	if !missing.Build {
		t.Error("reused an image that is not on the host")
	}

	// The tag IS the digest, so an image built from different inputs cannot be mistaken
	// for this one: it answers a different tag and the lookup misses.
	writeFile(t, root, "main.go", "package main // edited\n")
	changed := probeRunner(t, root, Args{}).resolveSmokeImage(probeSpec, present)
	if !changed.Build {
		t.Errorf("reused the image built from the OLD inputs after the context changed: %s", changed.Reason)
	}
}

func TestRebuildFlagBuildsEvenWhenTheImageIsAlreadyThere(t *testing.T) {
	root := writeProbeContext(t)
	always := func(string) bool { return true }

	image := probeRunner(t, root, Args{Rebuild: true}).resolveSmokeImage(probeSpec, always)
	if !image.Build {
		t.Error("--rebuild did not force a build")
	}
	if !strings.Contains(image.Reason, "--rebuild") {
		t.Errorf("reason = %q, want it to name --rebuild", image.Reason)
	}
}

func TestSmokeImageSuppliedByEnvIsNeverBuilt(t *testing.T) {
	t.Setenv(releaseSmokeImageSpec.EnvVar, "my-own:tag")
	image := probeRunner(t, writeProbeContext(t), Args{}).resolveSmokeImage(releaseSmokeImageSpec, func(string) bool { return false })
	if image.Build {
		t.Error("built over an image the operator supplied")
	}
	if image.Ref() != "my-own:tag" {
		t.Errorf("ref = %q, want the supplied image", image.Ref())
	}
}

// A suspicion that a run used a stale image has to be answerable from the log. Every
// decision therefore carries a reason, and every reason says which way it went.
func TestEveryImageDecisionExplainsItself(t *testing.T) {
	root := writeProbeContext(t)
	for _, tc := range []struct {
		name   string
		args   Args
		exists func(string) bool
		want   string
	}{
		{name: "reuse", args: Args{}, exists: func(string) bool { return true }, want: "reusing:"},
		{name: "build", args: Args{}, exists: func(string) bool { return false }, want: "building:"},
		{name: "forced", args: Args{Rebuild: true}, exists: func(string) bool { return true }, want: "building:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			image := probeRunner(t, root, tc.args).resolveSmokeImage(probeSpec, tc.exists)
			if !strings.HasPrefix(image.Reason, tc.want) {
				t.Errorf("reason = %q, want it to start with %q", image.Reason, tc.want)
			}
		})
	}
}

// An unreadable build input must not silently become "nothing changed". Failing closed
// costs a rebuild; failing open ships a stale image.
func TestAnUndigestableInputForcesABuildRatherThanAReuse(t *testing.T) {
	root := writeProbeContext(t)
	missing := smokeImageSpec{Repo: "probe-image", Dockerfile: "no/such/Dockerfile"}

	image := probeRunner(t, root, Args{}).resolveSmokeImage(missing, func(string) bool { return true })
	if !image.Build {
		t.Error("reused an image although the build inputs could not be digested")
	}
	if !strings.Contains(image.Reason, "could not digest") {
		t.Errorf("reason = %q, want it to say the digest failed", image.Reason)
	}
}

// AC-4 is only met if the decision reaches the RUN OUTPUT. A reason field nothing prints
// leaves a stale-image suspicion exactly where it was: answerable only by deleting tags.
func TestDockerSmokeRunNamesEachImageAndWhy(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--suite", "smoke", "--filter", "docker", "--dry-run"}, &stdout, &stderr); code != 0 {
		t.Fatalf("Run returned %d, stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"image pinchtab-release-smoke:dry-run",
		"image pinchtab-chrome-cft-smoke:dry-run",
		"building: dry run does not inspect local images",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the run does not state the image decision %q:\n%s", want, out)
		}
	}
}

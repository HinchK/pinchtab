package e2e

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// smokeImageSpec is one image the docker smoke lane needs, described by what it is built
// FROM rather than by a tag. The tag is derived from those inputs, so an image whose
// inputs changed cannot collide with the image built from the old ones.
type smokeImageSpec struct {
	Repo       string
	Dockerfile string
	Platform   string
	EnvVar     string
}

// smokeImage is the resolved decision for one image: which tag to use, whether the lane
// has to build it, and the sentence the run prints so a stale-image suspicion is
// answerable from the log.
type smokeImage struct {
	Repo   string
	Tag    string
	Build  bool
	Reason string
}

func (i smokeImage) Ref() string {
	if i.Tag == "" {
		return i.Repo
	}
	return i.Repo + ":" + i.Tag
}

// resolveSmokeImage decides whether the lane builds this image or reuses one already on
// the host. The tag IS the digest of the build inputs, so "an image with this tag exists"
// and "the inputs are unchanged" are the same question — a stale image cannot answer it,
// because different inputs produce a different tag and therefore a miss.
func (r *Runner) resolveSmokeImage(spec smokeImageSpec, exists func(ref string) bool) smokeImage {
	if custom := strings.TrimSpace(os.Getenv(spec.EnvVar)); custom != "" {
		return smokeImage{Repo: custom, Reason: "reusing: supplied via " + spec.EnvVar}
	}

	if r.args.DryRun {
		return smokeImage{Repo: spec.Repo, Tag: "dry-run", Build: true, Reason: "building: dry run does not inspect local images"}
	}

	digest, err := buildInputsDigest(r.repoRoot, spec)
	if err != nil {
		return smokeImage{
			Repo:   spec.Repo,
			Tag:    "no-digest",
			Build:  true,
			Reason: fmt.Sprintf("building: could not digest the build inputs (%v)", err),
		}
	}

	image := smokeImage{Repo: spec.Repo, Tag: digest}
	switch {
	case r.args.Rebuild:
		image.Build = true
		image.Reason = "building: --rebuild was requested"
	case exists(image.Ref()):
		image.Reason = "reusing: build inputs unchanged since this image was built"
	default:
		image.Build = true
		image.Reason = "building: no local image for these build inputs"
	}
	return image
}

func (r *Runner) localImageExists(ref string) bool {
	cmd := exec.Command("docker", "image", "inspect", ref) // #nosec G204 -- ref is derived from a fixed repo name and a hex digest
	cmd.Dir = r.repoRoot
	return cmd.Run() == nil
}

// buildInputsDigest hashes everything `docker build` would consume for this image: the
// Dockerfile, the platform it is pinned to, and the content of every file in the build
// context. Content, not mtime — a checkout, a stash pop or a touch must not read as a
// change, and an edit that restores a file byte-for-byte must not read as one either.
func buildInputsDigest(repoRoot string, spec smokeImageSpec) (string, error) {
	excluded, err := contextExcludedDirs(filepath.Join(repoRoot, dockerIgnoreFile))
	if err != nil {
		return "", err
	}

	sum := sha256.New()
	_, _ = fmt.Fprintf(sum, "dockerfile:%s\nplatform:%s\n", spec.Dockerfile, spec.Platform)

	// The Dockerfile is hashed by name AND content here rather than being left to the
	// context walk, because -f can point outside the context: the CFT Dockerfile lives
	// under tests/, which .dockerignore excludes, so the walk never reaches the one file
	// whose every line is a build input.
	dockerfileSum, err := fileDigest(filepath.Join(repoRoot, spec.Dockerfile))
	if err != nil {
		return "", err
	}
	_, _ = fmt.Fprintf(sum, "dockerfile-content:%s\n", dockerfileSum)

	files, err := contextFiles(repoRoot, excluded)
	if err != nil {
		return "", err
	}
	for _, rel := range files {
		fileSum, err := fileDigest(filepath.Join(repoRoot, rel))
		if err != nil {
			return "", err
		}
		_, _ = fmt.Fprintf(sum, "%s %s\n", fileSum, rel)
	}
	return hex.EncodeToString(sum.Sum(nil))[:16], nil
}

const dockerIgnoreFile = ".dockerignore"

// contextExcludedDirs reads the directory prefixes .dockerignore excludes. It deliberately
// understands only unnegated `dir/` entries and treats every other pattern as included.
//
// The asymmetry is the point. Digesting a file docker ignores costs a rebuild that was not
// strictly needed, and the run says so. MISSING a file docker sends is a stale image
// passing a run it should have failed — silent, and the exact failure this card warns
// about. So anything this parser cannot decide exactly is included.
func contextExcludedDirs(path string) (map[string]bool, error) {
	file, err := os.Open(path) // #nosec G304 -- path is the repo's own .dockerignore
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var dirs []string
	var negated []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if after, ok := strings.CutPrefix(line, "!"); ok {
			negated = append(negated, strings.TrimSpace(after))
			continue
		}
		if strings.HasSuffix(line, "/") && !strings.ContainsAny(line, "*?[") {
			dirs = append(dirs, strings.TrimSuffix(line, "/"))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	excluded := map[string]bool{}
	for _, dir := range dirs {
		if !anyNegationReaches(dir, negated) {
			excluded[dir] = true
		}
	}
	return excluded, nil
}

func anyNegationReaches(dir string, negated []string) bool {
	for _, n := range negated {
		if n == dir || strings.HasPrefix(n, dir+"/") {
			return true
		}
	}
	return false
}

func contextFiles(repoRoot string, excludedDirs map[string]bool) ([]string, error) {
	var files []string
	err := filepath.WalkDir(repoRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if entry.IsDir() {
			// Matched against the context-root-relative path, exactly as docker matches
			// .dockerignore. A bare `node_modules/` entry excludes the root one and NOT
			// a nested `npm/node_modules` — which is why this repo's .dockerignore lists
			// `dashboard/node_modules/` on its own line. Matching on the base name
			// instead would skip every directory of that name at any depth: files docker
			// sends, absent from the digest, and a stale image reused for changed inputs.
			if excludedDirs[rel] {
				return filepath.SkipDir
			}
			return nil
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// fileDigest hashes what the image ends up containing for this path: the executable bit
// and then either the link target or the content. A symlink is hashed by where it points
// rather than followed, so a retarget moves the digest and a loop cannot hang the walk.
func fileDigest(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}

	sum := sha256.New()
	// The exec bit travels into the image, so a chmod +x is a change to what was built
	// even when every byte is identical.
	executable := 0
	if info.Mode().Perm()&0o111 != 0 {
		executable = 1
	}
	_, _ = fmt.Fprintf(sum, "exec:%d\n", executable)

	if info.Mode()&os.ModeSymlink != 0 {
		target, linkErr := os.Readlink(path)
		if linkErr != nil {
			return "", linkErr
		}
		_, _ = fmt.Fprintf(sum, "symlink:%s", target)
		return hex.EncodeToString(sum.Sum(nil)), nil
	}

	file, err := os.Open(path) // #nosec G304 -- path comes from walking the repo's own build context
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

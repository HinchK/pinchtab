// Package devtools holds repo-level invariants about this project's development
// tooling — properties of how the suite is invoked and reported, which belong to no
// product package. It is test-only on purpose: there is nothing here to import.
package devtools

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gotestsumBinary is the token that marks a line as an invocation rather than a mention.
// Lines that merely install, locate or name the tool are excluded below, so a resolver
// script that spells the word a dozen times contributes no false positives.
const gotestsumBinary = "gotestsum"

// hiddenOutputSummary is the flag under ban. gotestsum's output summary is the section
// that carries file:line and expected-versus-actual beneath each failing test name;
// hiding it reduces a failure to "=== FAIL: pkg TestName (0.9s)" with the cause nowhere
// in the log.
const hiddenOutputSummary = "--hide-summary=output"

// The rule is deliberately ONE-SIDED: no invocation may hide the output summary. It is
// NOT "the invocations must agree". They are meant to differ — scripts/test.sh hides the
// skipped section because print_skipped_tests re-renders it with each skip reason, and CI
// has no equivalent, so an agreement rule would be satisfiable by making CI hide skipped
// and lose those reasons. That is the wrong fix passing the check.
//
// Found by scanning rather than by naming the two files known today: this repo has
// already been bitten by a one-owner guard that checked two paths by name and was blind
// to everything else, and a third invocation added later must be covered by construction.
func TestNoGotestsumInvocationHidesTheOutputSummary(t *testing.T) {
	root := repoRoot(t)

	var invocations, offenders int
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipScanDir(d.Name()) && path != root {
				return fs.SkipDir
			}
			return nil
		}
		if !scannableForInvocations(path) {
			return nil
		}
		body, readErr := os.ReadFile(path) // #nosec G304 -- files walked from this repo's own tree.
		if readErr != nil {
			return readErr
		}
		rel, _ := filepath.Rel(root, path)
		for i, line := range strings.Split(string(body), "\n") {
			if !isGotestsumInvocation(line) {
				continue
			}
			invocations++
			if strings.Contains(line, hiddenOutputSummary) {
				offenders++
				t.Errorf("%s:%d hides the gotestsum output summary, so a failure there prints a bare test name with the cause nowhere in the log:\n\t%s",
					rel, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("cannot scan the repo, so this guard checks nothing: %v", err)
	}

	if invocations == 0 {
		t.Fatalf("found no gotestsum invocation anywhere under %s; a rename or a move has made this guard vacuous — re-point it at whatever runs the suite rather than deleting it", root)
	}
	if offenders == 0 && invocations < 2 {
		t.Errorf("found only %d gotestsum invocation; both the CI workflow and the test script are expected to run one, so the scan is probably missing a file type", invocations)
	}
}

// isGotestsumInvocation keeps the ban on lines that RUN the tool. The resolver in
// scripts/test.sh and the doctor check both mention it repeatedly while installing or
// locating it, and a ban that fired on those would be noise — a guard that cries wolf
// gets deleted, which is how the gap it protects comes back.
func isGotestsumInvocation(line string) bool {
	trimmed := strings.TrimSpace(line)
	// Case-insensitive because a shell script invokes the tool through "$GOTESTSUM_BIN",
	// which a lowercase-only match misses entirely — the scan then sees one invocation
	// where there are two, and the floor below is what caught that.
	if strings.HasPrefix(trimmed, "#") || !strings.Contains(strings.ToLower(trimmed), gotestsumBinary) {
		return false
	}
	// An invocation passes flags to the tool; every mention that merely installs or
	// locates it does not.
	return strings.Contains(trimmed, "--format=") || strings.Contains(trimmed, "--hide-summary=")
}

func scannableForInvocations(path string) bool {
	switch filepath.Ext(path) {
	case ".yml", ".yaml", ".sh":
		return true
	}
	return filepath.Base(path) == "dev"
}

func skipScanDir(name string) bool {
	switch name {
	case ".git", "node_modules", "dist", "vendor", ".tools":
		return true
	}
	return false
}

// repoRoot walks up to the directory holding go.mod rather than counting "../" hops, so
// moving this package does not silently point the scan at a subtree.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above the working directory; cannot locate the repo root")
		}
		dir = parent
	}
}

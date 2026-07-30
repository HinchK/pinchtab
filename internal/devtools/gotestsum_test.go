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

const gotestsumBinary = "gotestsum"

const hideSummaryFlag = "--hide-summary="

const bannedSummarySection = "output"

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
			if hidesOutputSummary(line) {
				offenders++
				t.Errorf("%s:%d hides the gotestsum output summary, so a failure there prints a bare test name with the cause nowhere in the log; the rule is one-sided — hiding other sections is fine, hiding output is not:\n\t%s",
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

// hidesOutputSummary parses --hide-summary as the comma-separated list gotestsum accepts,
// so "skipped,output" is caught as surely as "output". gotestsum rejects every other
// spelling — a space instead of =, a single dash, uppercase section names — so the fields
// of the =-joined lowercase list are the whole class.
func hidesOutputSummary(line string) bool {
	rest := line
	for {
		at := strings.Index(rest, hideSummaryFlag)
		if at < 0 {
			return false
		}
		rest = rest[at+len(hideSummaryFlag):]
		value := rest
		if end := strings.IndexAny(value, " \t"); end >= 0 {
			value = value[:end]
		}
		for _, section := range strings.Split(value, ",") {
			if strings.TrimSpace(section) == bannedSummarySection {
				return true
			}
		}
	}
}

func isGotestsumInvocation(line string) bool {
	trimmed := strings.TrimSpace(line)
	// Case-insensitive: scripts/test.sh invokes the tool through "$GOTESTSUM_BIN", which a
	// lowercase-only match misses entirely.
	if strings.HasPrefix(trimmed, "#") || !strings.Contains(strings.ToLower(trimmed), gotestsumBinary) {
		return false
	}
	return strings.Contains(trimmed, "--format=") || strings.Contains(trimmed, hideSummaryFlag)
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

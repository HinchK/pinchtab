package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The helper layer is small; if the walk finds fewer files than this it matched
// the wrong directory and the rule below would pass for the wrong reason.
const minHelperFilesScanned = 5

// pipesIntoEarlyExitReader matches `… | grep -q …`, in any of the flag spellings
// used in this tree (-q, -qi, -qE, -qF), reached through a pipe.
//
// Under `set -o pipefail` — which helpers/base.sh sets for every scenario — this
// shape is a race, not a style preference. grep -q exits the moment it matches,
// which closes the read end of the pipe; if the writer has not finished, it dies
// of SIGPIPE with status 141 and pipefail promotes that to the status of the
// whole pipeline. The assertion then reads the opposite of the truth:
//
//	assert_output_contains      reports a FAILURE on output that does contain the needle
//	assert_output_not_contains  reports a PASS on output that does contain the needle
//
// The second is the dangerous one: it is how "token not leaked to stdout" is
// checked. Both flip once the payload outgrows the pipe buffer, so they are
// invisible until an endpoint's output grows.
//
// Read from a here-string instead: `grep -q -- "$needle" <<<"$haystack"`.
var pipesIntoEarlyExitReader = regexp.MustCompile(`\|\s*grep\s+-[a-zA-Z]*q`)

func loadHelperFiles(t *testing.T) map[string]string {
	t.Helper()

	files := map[string]string{}
	entries, err := os.ReadDir("helpers")
	if err != nil {
		t.Fatalf("cannot read helpers/, so this rule would check nothing: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sh") {
			continue
		}
		path := filepath.Join("helpers", e.Name())
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		files[path] = string(body)
	}
	if len(files) < minHelperFilesScanned {
		t.Fatalf("scanned %d helper files, want at least %d; the walk matched almost nothing and this rule would pass vacuously", len(files), minHelperFilesScanned)
	}
	return files
}

func TestNoHelperPipesIntoGrepQ(t *testing.T) {
	var offenders []string
	for path, body := range loadHelperFiles(t) {
		for i, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			if pipesIntoEarlyExitReader.MatchString(line) {
				offenders = append(offenders, path+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("piping into `grep -q` under `set -o pipefail` inverts the assertion "+
			"once the payload outgrows the pipe buffer: grep exits early, the writer takes "+
			"SIGPIPE, and pipefail returns 141 for a pipeline that actually matched. Read "+
			"from a here-string instead — `grep -q -- \"$needle\" <<<\"$haystack\"`.\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

// helpers/base.sh sets pipefail for every scenario, which is what makes the rule
// above load-bearing rather than stylistic. If that line ever goes away the rule
// should be revisited, not silently kept.
func TestHelpersStillSetPipefail(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("helpers", "base.sh"))
	if err != nil {
		t.Fatalf("read helpers/base.sh: %v", err)
	}
	if !strings.Contains(string(body), "pipefail") {
		t.Fatal("helpers/base.sh no longer sets pipefail; TestNoHelperPipesIntoGrepQ was written " +
			"for that mode and its rationale needs rechecking before it is trusted or removed")
	}
}

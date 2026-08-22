package handlers

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/bridge"
)

func parseQuery(t *testing.T, raw string) (SnapshotCostControls, error) {
	t.Helper()
	q, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("bad probe query %q: %v", raw, err)
	}
	return ParseSnapshotCostControls(q)
}

// Every cost control used to fail OPEN toward the expensive answer: an unrecognised format
// fell through to json, a mis-cased filter returned the whole tree instead of the
// interactive subset, and an unparseable budget or depth was dropped. A caller that
// mistyped its own cost control was not told, only charged more.
func TestInvalidCostControlsAreRejectedRatherThanIgnored(t *testing.T) {
	for _, tc := range []struct {
		name      string
		query     string
		wantNamed []string
	}{
		{
			name:      "unknown format does not silently become json",
			query:     "format=compct",
			wantNamed: []string{"json", "compact", "text", "yaml"},
		},
		{
			name:      "format that is a real word but not ours",
			query:     "format=xml",
			wantNamed: []string{"compact"},
		},
		{
			name:      "unknown filter does not silently become the whole tree",
			query:     "filter=interactives",
			wantNamed: []string{"all", "interactive"},
		},
		{
			name:      "unparseable maxTokens does not silently drop the budget",
			query:     "maxTokens=lots",
			wantNamed: []string{"positive"},
		},
		{
			name:      "zero maxTokens is not a budget",
			query:     "maxTokens=0",
			wantNamed: []string{"positive"},
		},
		{
			name:      "negative maxTokens is not a budget",
			query:     "maxTokens=-5",
			wantNamed: []string{"positive"},
		},
		{
			name:      "unparseable depth does not silently drop the limit",
			query:     "depth=deep",
			wantNamed: []string{"-1"},
		},
		{
			name:      "depth below the no-limit sentinel is meaningless",
			query:     "depth=-2",
			wantNamed: []string{"-1"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseQuery(t, tc.query)
			if err == nil {
				t.Fatalf("%s was accepted; the caller gets an answer to a question it did not ask", tc.query)
			}
			for _, want := range tc.wantNamed {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the rejection does not name %q, so the caller cannot tell what to send instead: %v", want, err)
				}
			}
			// The rejection has to quote what was actually sent, or a caller with a
			// generated query cannot find which value was wrong.
			sent := strings.SplitN(tc.query, "=", 2)[1]
			if !strings.Contains(err.Error(), sent) {
				t.Errorf("the rejection does not quote the offending value %q: %v", sent, err)
			}
		})
	}
}

// A single capital letter used to turn an interactive snapshot into a full-tree one.
func TestFormatAndFilterAreCaseAndWhitespaceInsensitive(t *testing.T) {
	for _, query := range []string{
		"format=COMPACT&filter=INTERACTIVE",
		"format=Compact&filter=Interactive",
		"format=+compact+&filter=+interactive+",
		"format=%09compact%0A&filter=%20interactive%20",
	} {
		t.Run(query, func(t *testing.T) {
			got, err := parseQuery(t, query)
			if err != nil {
				t.Fatalf("rejected a value that differs only in case or spacing: %v", err)
			}
			if got.Format != "compact" {
				t.Errorf("format = %q, want compact", got.Format)
			}
			if got.Filter != bridge.FilterInteractive {
				t.Errorf("filter = %q, want %q — a mis-cased filter used to return the whole tree", got.Filter, bridge.FilterInteractive)
			}
		})
	}
}

func TestCostControlDefaultsWhenUnset(t *testing.T) {
	got, err := parseQuery(t, "tabId=abc")
	if err != nil {
		t.Fatal(err)
	}
	if got.Format != "json" {
		t.Errorf("format = %q, want json", got.Format)
	}
	if got.Filter != "" {
		t.Errorf("filter = %q, want the whole tree", got.Filter)
	}
	if got.MaxTokens != -1 || got.MaxDepth != -1 {
		t.Errorf("maxTokens = %d, depth = %d, want -1 for both", got.MaxTokens, got.MaxDepth)
	}
	if len(got.Ignored) != 0 {
		t.Errorf("ignored = %v, want none", got.Ignored)
	}
}

// "all" is the documented spelling of the default and has to be accepted explicitly, not
// merely tolerated by falling through the unknown-filter branch.
func TestFilterAllIsTheWholeTree(t *testing.T) {
	got, err := parseQuery(t, "filter=all")
	if err != nil {
		t.Fatalf("filter=all was rejected: %v", err)
	}
	if got.Filter != "" {
		t.Errorf("filter = %q, want the whole tree", got.Filter)
	}
}

// The decision on this card: unknown NAMES are reported, not rejected, so version skew in
// the normal direction — a newer client sending a parameter an older server has not learned
// — keeps working while a typo stops being invisible.
func TestUnknownParamsAreReportedNotRejected(t *testing.T) {
	got, err := parseQuery(t, "tabId=abc&compact=true&interactive=true&format=compact")
	if err != nil {
		t.Fatalf("an unknown parameter was rejected; version skew would break: %v", err)
	}
	if want := []string{"compact", "interactive"}; !equalStrings(got.Ignored, want) {
		t.Errorf("ignored = %v, want %v — this is the disclosure that would have caught `compact=true` in the CLI", got.Ignored, want)
	}
	if got.Format != "compact" {
		t.Errorf("format = %q, want the valid parameter still honoured", got.Format)
	}
}

// The failure this guard exists for is a parameter the request path DOES read being
// reported as ignored: `browser` is consumed by the routing prelude rather than by
// HandleSnapshot, so a known-params set derived from one file alone gets it wrong and every
// browser-routed call grows a false disclosure.
func TestParamsTheRequestPathReadsAreNotReportedAsIgnored(t *testing.T) {
	got, err := parseQuery(t, "tabId=abc&browser=chrome&filter=interactive&format=compact&depth=3&maxTokens=500&diff=true&noAnimations=true&selector=%23main&output=file&path=out.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Ignored) != 0 {
		t.Errorf("ignored = %v, but every one of those is read on the snapshot request path", got.Ignored)
	}
}

var queryGetPattern = regexp.MustCompile(`Query\(\)\.Get\("([^"]+)"\)`)

// snapshotPathSources are the files whose Query().Get calls can run for a /snapshot
// request. Listed rather than derived because the call graph decides membership.
var snapshotPathSources = []string{"snapshot.go", "read_prelude.go"}

// notOnTheSnapshotPath are parameters those files read from a function /snapshot never
// calls. They belong OUT of snapshotKnownParams: /snapshot genuinely ignores them, so
// reporting them back is the correct answer rather than a false disclosure. Checked in
// both directions below, because an exemption for something the source no longer reads is
// how a guard quietly stops guarding.
var notOnTheSnapshotPath = map[string]string{
	"frameId": "read by resolveTargetFrameID, which /inspect and /text call and HandleSnapshot does not; /snapshot scopes frames from tab state instead",
}

// A hand-maintained allowlist drifts the moment a parameter is added, and it drifts
// SILENTLY — the only symptom is a caller told its valid parameter was ignored. This reads
// the parameters straight out of the source instead of trusting the list to have kept up.
func TestKnownParamsCoversEveryParamTheHandlerReads(t *testing.T) {
	var missing []string
	seen := 0
	exemptionsUsed := map[string]bool{}
	for _, name := range snapshotPathSources {
		body, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("cannot read %s, so this guard would check nothing: %v", name, err)
		}
		for _, match := range queryGetPattern.FindAllStringSubmatch(string(body), -1) {
			seen++
			param := match[1]
			if _, exempt := notOnTheSnapshotPath[param]; exempt {
				exemptionsUsed[param] = true
				continue
			}
			if !snapshotKnownParams[param] {
				missing = append(missing, name+": "+param)
			}
		}
	}
	if seen < len(snapshotPathSources) {
		t.Fatalf("found %d query reads across %v; the scan matched almost nothing and would pass vacuously", seen, snapshotPathSources)
	}
	if len(missing) > 0 {
		t.Errorf("these parameters are read on the snapshot request path but are absent from snapshotKnownParams, so a caller sending them is wrongly told they were ignored:\n  %s",
			strings.Join(missing, "\n  "))
	}
	for param, why := range notOnTheSnapshotPath {
		if !exemptionsUsed[param] {
			t.Errorf("%q is exempted (%s) but no longer read by %v; drop the exemption, or re-point it, rather than leaving it to excuse nothing", param, why, snapshotPathSources)
		}
		if snapshotKnownParams[param] {
			t.Errorf("%q is both exempted and listed as known; one of the two is wrong", param)
		}
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

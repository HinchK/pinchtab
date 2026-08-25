package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleResults() []suiteTestResult {
	return []suiteTestResult{
		{Name: "[actions-basic] pinchtab click <button>", Status: "passed", DurationMs: 790},
		{Name: "[actions-basic] pinchtab press <key>", Status: "passed", DurationMs: 20},
		{Name: "[audit-basic] sitemap mode discovers pages", Status: "failed", DurationMs: 6050},
		{Name: "ungrouped result with no scenario", Status: "passed", DurationMs: 140},
	}
}

func TestBuildSuiteTimingsGroupsByScenarioAndTotalsReconcile(t *testing.T) {
	timings := buildSuiteTimings("api-extended", "multiCompose", "chrome", "2026-01-01T00:00:00Z", sampleResults())

	if timings.Tests != 4 {
		t.Errorf("Tests = %d, want 4", timings.Tests)
	}

	var recordSum int64
	for _, record := range timings.Records {
		recordSum += record.Ms
	}
	var scenarioSum int64
	for _, scenario := range timings.Scenarios {
		scenarioSum += scenario.Ms
	}
	if timings.TotalMs != recordSum || timings.TotalMs != scenarioSum {
		t.Errorf("totals disagree: suite=%d records=%d scenarios=%d; a totals file whose parts do not add up cannot be diffed against a later run",
			timings.TotalMs, recordSum, scenarioSum)
	}
	if timings.TotalMs != 7000 {
		t.Errorf("TotalMs = %d, want 7000", timings.TotalMs)
	}

	if got := len(timings.Scenarios); got != 3 {
		t.Fatalf("scenarios = %d, want 3", got)
	}
	if timings.Scenarios[0].Scenario != "audit-basic" || timings.Scenarios[0].Ms != 6050 {
		t.Errorf("scenarios are not ordered slowest-first: got %+v", timings.Scenarios[0])
	}
	for _, scenario := range timings.Scenarios {
		if scenario.Scenario == "actions-basic" && scenario.Tests != 2 {
			t.Errorf("actions-basic tests = %d, want 2", scenario.Tests)
		}
	}
}

func TestBuildSuiteTimingsStampsEveryRecordWithItsRunConditions(t *testing.T) {
	timings := buildSuiteTimings("api-extended", "multiCompose", "cloak", "2026-01-01T00:00:00Z", sampleResults())

	for _, record := range timings.Records {
		if record.Suite != "api-extended" || record.Stack != "multiCompose" || record.Provider != "cloak" {
			t.Fatalf("record %q lost its run conditions (%+v); records extracted from the file would silently mix stacks and providers across runs", record.Test, record)
		}
	}
	if timings.ComparisonFloorMs != comparisonFloorMs {
		t.Errorf("ComparisonFloorMs = %d, want %d recorded in the file so consumers read the rule from the data", timings.ComparisonFloorMs, comparisonFloorMs)
	}
}

func TestSplitScenarioReadsTheBracketPrefix(t *testing.T) {
	for _, tc := range []struct {
		name         string
		in           string
		wantScenario string
		wantTest     string
	}{
		{"bracketed", "[actions-basic] pinchtab click", "actions-basic", "pinchtab click"},
		{"no bracket", "plain name", "", "plain name"},
		{"unclosed bracket", "[broken name", "", "[broken name"},
		{"bracket later in name", "click [not a scenario]", "", "click [not a scenario]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scenario, test := splitScenario(tc.in)
			if scenario != tc.wantScenario || test != tc.wantTest {
				t.Errorf("splitScenario(%q) = (%q, %q), want (%q, %q)", tc.in, scenario, test, tc.wantScenario, tc.wantTest)
			}
		})
	}
}

func TestStackLabelNamesTheComposeFile(t *testing.T) {
	if got := stackLabel(singleCompose); got != "singleCompose" {
		t.Errorf("stackLabel(single) = %q", got)
	}
	if got := stackLabel(multiCompose); got != "multiCompose" {
		t.Errorf("stackLabel(multi) = %q", got)
	}
	if got := stackLabel(""); got != "none" {
		t.Errorf("stackLabel(\"\") = %q, want none", got)
	}
}

func TestRenderSlowestReportsTotalsAndTheComparisonFloor(t *testing.T) {
	timings := buildSuiteTimings("api-extended", "multiCompose", "chrome", "2026-01-01T00:00:00Z", sampleResults())
	out := renderSlowest(timings, 2)

	if !strings.Contains(out, "slowest 2 tests") {
		t.Errorf("missing the slowest header:\n%s", out)
	}
	if !strings.Contains(out, "sitemap mode discovers pages") {
		t.Errorf("slowest test is not listed first:\n%s", out)
	}
	if !strings.Contains(out, "per-scenario totals") {
		t.Errorf("per-scenario totals missing; gating is meant to happen on aggregates:\n%s", out)
	}
	if !strings.Contains(out, "under 100ms") {
		t.Errorf("the report does not state the comparison floor, so a reader will compute ratios on noise:\n%s", out)
	}
	if !strings.Contains(out, "1 test\n") {
		t.Errorf("scenario counts should read '1 test', not '1 tests':\n%s", out)
	}
}

func TestRenderSlowestCapsTheListAtTheNumberOfTests(t *testing.T) {
	timings := buildSuiteTimings("api", "singleCompose", "chrome", "", sampleResults())
	out := renderSlowest(timings, 500)
	if !strings.Contains(out, "slowest 4 tests") {
		t.Errorf("asking for more tests than the run holds should report what exists:\n%s", out)
	}
}

func TestRunSlowestReadsTheCheckedInJSONWithoutRunningAnything(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tests/e2e/results"), 0o755); err != nil {
		t.Fatal(err)
	}
	timings := buildSuiteTimings("api-extended", "multiCompose", "chrome", "2026-01-01T00:00:00Z", sampleResults())
	encoded, err := json.MarshalIndent(timings, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, resultsPath("timings", "api-extended", "json")), encoded, 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runSlowest("api-extended", 3, root, &out); err != nil {
		t.Fatalf("runSlowest() error = %v", err)
	}
	if !strings.Contains(out.String(), "stack: multiCompose") {
		t.Errorf("run conditions missing from the report:\n%s", out.String())
	}
}

func TestRunSlowestSaysWhichSuiteHasNoTimingsYet(t *testing.T) {
	err := runSlowest("api-extended", 3, t.TempDir(), &bytes.Buffer{})
	if err == nil {
		t.Fatal("runSlowest() on a suite that has never run reported success")
	}
	if !strings.Contains(err.Error(), "api-extended") {
		t.Errorf("error does not name the suite: %v", err)
	}
}

func TestSuiteTimingsPathSitsBesideTheMarkdownReport(t *testing.T) {
	def := suiteDescriptor{Name: "api-extended", Compose: multiCompose}.build()
	if def.Timings != "tests/e2e/results/timings-api-extended.json" {
		t.Errorf("Timings = %q", def.Timings)
	}
}

func TestParseArgsValidatesSlowest(t *testing.T) {
	for _, tc := range []struct {
		name    string
		argv    []string
		want    int
		wantErr bool
	}{
		{"positive", []string{"--slowest", "10"}, 10, false},
		{"equals form", []string{"--slowest=5"}, 5, false},
		{"zero", []string{"--slowest", "0"}, 0, true},
		{"negative", []string{"--slowest", "-3"}, 0, true},
		{"not a number", []string{"--slowest", "many"}, 0, true},
		{"absent", []string{"--suite", "api"}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, err := ParseArgs(tc.argv)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseArgs(%v) error = nil, want an error", tc.argv)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseArgs(%v) error = %v", tc.argv, err)
			}
			if args.Slowest != tc.want {
				t.Errorf("Slowest = %d, want %d", args.Slowest, tc.want)
			}
		})
	}
}

func TestPrepareSuiteResultsClearsStaleTimings(t *testing.T) {
	root := t.TempDir()
	def := suiteDescriptor{Name: "api-extended", Compose: multiCompose}.build()
	if err := os.MkdirAll(filepath.Join(root, "tests/e2e/results"), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(root, def.Timings)
	if err := os.WriteFile(stale, []byte(`{"tests":1}`), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &Runner{repoRoot: root}
	r.prepareSuiteResults(def)

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale timings survived the run preparation (stat err = %v); a run that dies before writing reports would leave the previous run's numbers looking current", err)
	}
}

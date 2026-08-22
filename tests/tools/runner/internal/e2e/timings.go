package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// comparisonFloorMs is the duration below which a run-to-run ratio is noise:
// measured median swing between runs is ~16%, so a 10ms→20ms move reads as
// +100% and means nothing. Sub-floor tests stay in the records and in the
// totals; only ratio comparisons must skip them.
const comparisonFloorMs = 100

type timingRecord struct {
	Suite    string `json:"suite"`
	Stack    string `json:"stack"`
	Provider string `json:"provider"`
	Scenario string `json:"scenario"`
	Test     string `json:"test"`
	Ms       int64  `json:"ms"`
	Status   string `json:"status"`
}

type scenarioTotal struct {
	Scenario string `json:"scenario"`
	Tests    int    `json:"tests"`
	Ms       int64  `json:"ms"`
}

type suiteTimings struct {
	Suite             string          `json:"suite"`
	Stack             string          `json:"stack"`
	Provider          string          `json:"provider"`
	Timestamp         string          `json:"timestamp"`
	ComparisonFloorMs int64           `json:"comparisonFloorMs"`
	Tests             int             `json:"tests"`
	TotalMs           int64           `json:"totalMs"`
	Scenarios         []scenarioTotal `json:"scenarios"`
	Records           []timingRecord  `json:"records"`
}

// stackLabel names the compose file the suite ran under. Run-to-run comparisons
// that mix stacks are measuring contention, not the change under test.
func stackLabel(compose string) string {
	switch compose {
	case singleCompose:
		return "singleCompose"
	case multiCompose:
		return "multiCompose"
	case "":
		return "none"
	default:
		return compose
	}
}

// splitScenario reads the "[scenario] test name" shape the E2E_RESULT stream
// already carries, so the scenario grouping needs no second source of truth.
func splitScenario(name string) (scenario, test string) {
	if !strings.HasPrefix(name, "[") {
		return "", name
	}
	end := strings.Index(name, "]")
	if end < 0 {
		return "", name
	}
	return name[1:end], strings.TrimSpace(name[end+1:])
}

func buildSuiteTimings(suite, stack, provider, timestamp string, results []suiteTestResult) suiteTimings {
	timings := suiteTimings{
		Suite:             suite,
		Stack:             stack,
		Provider:          provider,
		Timestamp:         timestamp,
		ComparisonFloorMs: comparisonFloorMs,
		Tests:             len(results),
		Records:           make([]timingRecord, 0, len(results)),
	}

	totals := map[string]*scenarioTotal{}
	var order []string
	for _, result := range results {
		scenario, test := splitScenario(result.Name)
		timings.TotalMs += result.DurationMs
		timings.Records = append(timings.Records, timingRecord{
			Suite:    suite,
			Stack:    stack,
			Provider: provider,
			Scenario: scenario,
			Test:     test,
			Ms:       result.DurationMs,
			Status:   result.Status,
		})
		total, seen := totals[scenario]
		if !seen {
			total = &scenarioTotal{Scenario: scenario}
			totals[scenario] = total
			order = append(order, scenario)
		}
		total.Tests++
		total.Ms += result.DurationMs
	}

	for _, scenario := range order {
		timings.Scenarios = append(timings.Scenarios, *totals[scenario])
	}
	sort.Slice(timings.Scenarios, func(i, j int) bool {
		if timings.Scenarios[i].Ms != timings.Scenarios[j].Ms {
			return timings.Scenarios[i].Ms > timings.Scenarios[j].Ms
		}
		return timings.Scenarios[i].Scenario < timings.Scenarios[j].Scenario
	})
	return timings
}

func (r *Runner) writeSuiteTimings(def suiteDef, data suiteReportData, timestamp string) {
	provider := r.args.Provider
	if provider == "" {
		provider = defaultProvider
	}
	timings := buildSuiteTimings(def.Name, stackLabel(def.Compose), provider, timestamp, data.Results)

	encoded, err := json.MarshalIndent(timings, "", "  ")
	if err != nil {
		_, _ = fmt.Fprintf(r.stderr, "e2e: failed to encode %s: %v\n", def.Timings, err)
		return
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(filepath.Join(r.repoRoot, def.Timings), encoded, 0o644); err != nil {
		_, _ = fmt.Fprintf(r.stderr, "e2e: failed to write %s: %v\n", def.Timings, err)
	}
}

func loadSuiteTimings(repoRoot, suite string) (suiteTimings, error) {
	path := filepath.Join(repoRoot, resultsPath("timings", suite, "json"))
	data, err := os.ReadFile(path)
	if err != nil {
		return suiteTimings{}, fmt.Errorf("no timings for suite %q: %w (run the suite once to produce it)", suite, err)
	}
	var timings suiteTimings
	if err := json.Unmarshal(data, &timings); err != nil {
		return suiteTimings{}, fmt.Errorf("cannot parse %s: %w", path, err)
	}
	return timings, nil
}

func renderSlowest(timings suiteTimings, top int) string {
	var out strings.Builder
	fmt.Fprintf(&out, "suite: %s   stack: %s   browser: %s\n", timings.Suite, timings.Stack, timings.Provider)
	if timings.Timestamp != "" {
		fmt.Fprintf(&out, "run:   %s\n", timings.Timestamp)
	}
	fmt.Fprintf(&out, "total: %d tests in %s\n", timings.Tests, formatMs(timings.TotalMs))

	records := append([]timingRecord(nil), timings.Records...)
	sort.SliceStable(records, func(i, j int) bool { return records[i].Ms > records[j].Ms })
	if top > len(records) {
		top = len(records)
	}

	fmt.Fprintf(&out, "\nslowest %d tests\n", top)
	for _, record := range records[:top] {
		fmt.Fprintf(&out, "  %9s  [%s] %s\n", formatMs(record.Ms), record.Scenario, record.Test)
	}

	fmt.Fprintf(&out, "\nper-scenario totals\n")
	for _, scenario := range timings.Scenarios {
		fmt.Fprintf(&out, "  %9s  %-32s %s\n", formatMs(scenario.Ms), scenario.Scenario, pluralTests(scenario.Tests))
	}

	fmt.Fprintf(&out, "\nGate on the totals above, not on per-test ratios: run-to-run swing is ~16%%.\n")
	fmt.Fprintf(&out, "Exclude tests under %dms from any ratio — at that size the ratio is noise.\n", timings.ComparisonFloorMs)
	return out.String()
}

func pluralTests(n int) string {
	if n == 1 {
		return "1 test"
	}
	return fmt.Sprintf("%d tests", n)
}

func formatMs(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.2fs", float64(ms)/1000)
}

func runSlowest(suite string, top int, repoRoot string, stdout io.Writer) error {
	timings, err := loadSuiteTimings(repoRoot, suite)
	if err != nil {
		return err
	}
	if len(timings.Records) == 0 {
		return fmt.Errorf("timings for suite %q hold no test records", suite)
	}
	_, _ = io.WriteString(stdout, renderSlowest(timings, top))
	return nil
}

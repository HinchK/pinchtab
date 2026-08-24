package devtools

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	escalationScript   = "scripts/ci/detect-e2e-suites.sh"
	escalationMap      = "scripts/ci/e2e-escalation.map"
	escalationWorkflow = ".github/workflows/ci-e2e.yml"
	smokeWorkflow      = ".github/workflows/ci-smoke.yml"
)

func TestEscalationFollowsProductPathsAndTestPaths(t *testing.T) {
	root := repoRoot(t)

	cases := []struct {
		name    string
		changed []string
		want    []string
	}{
		{
			name:    "audit implementation with no scenario file",
			changed: []string{"internal/audit/run.go", "pkg/pinchtabaudit/client.go", "internal/handlers/audit.go", "cmd/pinchtab/cmd_audit.go"},
			want:    []string{"run_api_extended", "run_cli_extended"},
		},
		{
			name:    "audit CLI implementation rather than its command wrapper",
			changed: []string{"internal/cli/actions/actions_audit.go"},
			want:    []string{"run_api_extended", "run_cli_extended"},
		},
		{
			name:    "audit implementation alongside a basic scenario",
			changed: []string{"internal/audit/enrich.go", "tests/e2e/scenarios/api/audit-page-basic.sh"},
			want:    []string{"run_api_extended", "run_cli_extended"},
		},
		{
			name:    "compare implementation, which no API suite covers",
			changed: []string{"internal/cli/actions/actions_compare.go", "cmd/pinchtab/cmd_compare.go"},
			want:    []string{"run_cli_extended"},
		},
		{
			name:    "api extended scenario churn",
			changed: []string{"tests/e2e/scenarios/api/tabs-extended.sh"},
			want:    []string{"run_api_extended"},
		},
		{
			name:    "cli extended scenario churn",
			changed: []string{"tests/e2e/scenarios/cli/audit-pdf-extended.sh"},
			want:    []string{"run_cli_extended"},
		},
		{
			name:    "infra extended scenario churn",
			changed: []string{"tests/e2e/scenarios/infra/auth-extended.sh"},
			want:    []string{"run_infra_extended"},
		},
		{
			name:    "standalone scenario churn",
			changed: []string{"tests/e2e/scenarios/api/network-retain-body.sh"},
			want:    []string{"run_api_extended"},
		},
		{
			name:    "smoke inputs",
			changed: []string{"tests/e2e/scenarios/cli/tabs-smoke.sh", "Dockerfile"},
			want:    []string{"run_smoke"},
		},
		{
			name:    "basic scenario churn only",
			changed: []string{"tests/e2e/scenarios/api/tabs-basic.sh", "tests/e2e/scenarios/infra/network-basic.sh"},
			want:    nil,
		},
		{
			name:    "product paths no extended suite covers",
			changed: []string{"internal/dashboard/dashboard.go", "README.md", "internal/scroll/scroll.go"},
			want:    nil,
		},
		{
			name:    "empty diff",
			changed: nil,
			want:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := detectSuites(t, root, tc.changed)
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Errorf("%s escalated %v, want %v", strings.Join(tc.changed, ", "), got, tc.want)
			}
		})
	}
}

func TestEveryEscalationRuleMatchesATrackedPath(t *testing.T) {
	root := repoRoot(t)
	tracked := trackedPaths(t, root)

	for _, rule := range escalationRules(t, root) {
		pattern, err := regexp.Compile(rule)
		if err != nil {
			t.Errorf("rule %q is not a valid expression: %v", rule, err)
			continue
		}
		matched := false
		for _, path := range tracked {
			if pattern.MatchString(path) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("rule %q matches no tracked file, so the coverage it claims is not wired to anything; re-point it at the path that moved rather than leaving it", rule)
		}
	}
}

func TestTheWorkflowConsumesExactlyTheEmittedOutputs(t *testing.T) {
	root := repoRoot(t)
	body := readRepoFile(t, root, escalationWorkflow)

	if !strings.Contains(body, escalationScript) {
		t.Fatalf("%s does not invoke %s, so the mapping decides nothing", escalationWorkflow, escalationScript)
	}

	emitted := map[string]bool{}
	for _, name := range detectSuiteNames(t, root) {
		emitted[name] = true
		if !strings.Contains(body, "steps.changes.outputs."+name) {
			t.Errorf("%s emits %s but %s never exports it, so the suite it gates can never run", escalationScript, name, escalationWorkflow)
		}
	}

	consumed := regexp.MustCompile(`detect-changes\.outputs\.([a-z_]+)`)
	for _, match := range consumed.FindAllStringSubmatch(body, -1) {
		if !emitted[match[1]] {
			t.Errorf("%s gates a job on %s, which %s never emits, so the job never runs", escalationWorkflow, match[1], escalationScript)
		}
	}
}

func TestEveryEscalatableSuiteAlsoRunsWithoutADiffToClaimIt(t *testing.T) {
	root := repoRoot(t)
	automatic := suitesOnAutomaticTriggers(t, root)

	for _, name := range detectSuiteNames(t, root) {
		suite := strings.ReplaceAll(strings.TrimPrefix(name, "run_"), "_", "-")
		if !automatic[suite] {
			t.Errorf("the %s suite runs only when a diff escalates it or a human dispatches it, so a merge the map does not claim never exercises the coverage it holds; give it a job gated on github.event_name == '<event>' under an automatic trigger in %s or %s", suite, escalationWorkflow, smokeWorkflow)
		}
	}
}

type ciWorkflow struct {
	On   map[string]any   `yaml:"on"`
	Jobs map[string]ciJob `yaml:"jobs"`
}

type ciJob struct {
	If   string `yaml:"if"`
	Uses string `yaml:"uses"`
	With struct {
		Suite string `yaml:"suite"`
	} `yaml:"with"`
}

func suitesOnAutomaticTriggers(t *testing.T, root string) map[string]bool {
	t.Helper()
	automatic := map[string]bool{}
	for _, rel := range []string{escalationWorkflow, smokeWorkflow} {
		workflow := parseWorkflow(t, root, rel)
		for _, event := range automaticEvents(workflow) {
			for _, job := range workflow.Jobs {
				if suite := jobSuite(job); suite != "" && jobRunsOnEvent(job, event) {
					automatic[suite] = true
				}
			}
		}
	}
	return automatic
}

func automaticEvents(workflow ciWorkflow) []string {
	var events []string
	for event := range workflow.On {
		if event != "pull_request" && event != "workflow_dispatch" {
			events = append(events, event)
		}
	}
	sort.Strings(events)
	return events
}

func jobSuite(job ciJob) string {
	if job.With.Suite != "" {
		return job.With.Suite
	}
	if strings.Contains(job.Uses, "reusable-smoke") {
		return "smoke"
	}
	return ""
}

func jobRunsOnEvent(job ciJob, event string) bool {
	return job.If == "" || strings.Contains(job.If, "github.event_name == '"+event+"'")
}

func parseWorkflow(t *testing.T, root, rel string) ciWorkflow {
	t.Helper()
	var workflow ciWorkflow
	if err := yaml.Unmarshal([]byte(readRepoFile(t, root, rel)), &workflow); err != nil {
		t.Fatalf("cannot parse %s: %v", rel, err)
	}
	if len(workflow.On) == 0 || len(workflow.Jobs) == 0 {
		t.Fatalf("%s declares %d triggers and %d jobs, so every trigger guard over it would pass vacuously", rel, len(workflow.On), len(workflow.Jobs))
	}
	return workflow
}

func detectSuites(t *testing.T, root string, changed []string) []string {
	t.Helper()
	var escalated []string
	for _, line := range runDetect(t, root, changed) {
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("%s emitted %q, which is not a name=value pair", escalationScript, line)
		}
		if value == "true" {
			escalated = append(escalated, name)
		}
	}
	sort.Strings(escalated)
	return escalated
}

func detectSuiteNames(t *testing.T, root string) []string {
	t.Helper()
	var names []string
	for _, line := range runDetect(t, root, nil) {
		name, _, _ := strings.Cut(line, "=")
		names = append(names, name)
	}
	if len(names) == 0 {
		t.Fatalf("%s emitted no output, so every guard over it is vacuous", escalationScript)
	}
	return names
}

func runDetect(t *testing.T, root string, changed []string) []string {
	t.Helper()
	if readRepoFile(t, root, escalationScript) == "" || readRepoFile(t, root, escalationMap) == "" {
		t.Fatalf("%s and %s must both hold content, and reading them here is also what keeps a cached green from surviving an edit to either", escalationScript, escalationMap)
	}
	cmd := exec.Command("bash", filepath.Join(root, escalationScript))
	cmd.Stdin = strings.NewReader(strings.Join(changed, "\n"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s failed on %v: %v\n%s", escalationScript, changed, err, stderr.String())
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func escalationRules(t *testing.T, root string) []string {
	t.Helper()
	var rules []string
	for _, line := range strings.Split(readRepoFile(t, root, escalationMap), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rules = append(rules, strings.Fields(line)[0])
	}
	if len(rules) == 0 {
		t.Fatalf("%s holds no rule", escalationMap)
	}
	return rules
}

func trackedPaths(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("cannot list tracked files, so staleness is unprovable: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

func readRepoFile(t *testing.T, root, rel string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("cannot read %s: %v", rel, err)
	}
	return string(body)
}

type enrolledArea struct {
	name     string
	match    string
	suites   []string
	excluded map[string]string
}

var enrolledAreas = []enrolledArea{
	{
		name:   "audit",
		match:  `(?i)audit`,
		suites: []string{"run_api_extended", "run_cli_extended"},
		excluded: map[string]string{
			"internal/authn/audit.go": "security audit logging — AuditLog writes slog entries, a different sense of the word, covered by no audit scenario",
		},
	},
	{
		name:   "compare",
		match:  `(?i)compare`,
		suites: []string{"run_cli_extended"},
	},
}

func TestEveryEnrolledAreaIsCoveredByTheMap(t *testing.T) {
	root := repoRoot(t)
	tracked := trackedPaths(t, root)

	for _, area := range enrolledAreas {
		t.Run(area.name, func(t *testing.T) {
			pattern, err := regexp.Compile(area.match)
			if err != nil {
				t.Fatalf("area %q has an unusable match %q: %v", area.name, area.match, err)
			}

			var sources []string
			for _, path := range tracked {
				if isAreaSource(path) && pattern.MatchString(path) {
					sources = append(sources, path)
				}
			}
			if len(sources) < 2 {
				t.Fatalf("area %q matches %d source files; the match has stopped finding the area and this guard would pass vacuously", area.name, len(sources))
			}

			usedExclusion := map[string]bool{}
			for _, path := range sources {
				escalated := detectSuites(t, root, []string{path})
				if why, excluded := area.excluded[path]; excluded {
					usedExclusion[path] = true
					if len(escalated) > 0 {
						t.Errorf("%s is excluded from the %s area (%s) yet escalates %v; drop the exclusion rather than keeping a claim the map contradicts", path, area.name, why, escalated)
					}
					continue
				}
				for _, want := range area.suites {
					if !containsString(escalated, want) {
						t.Errorf("%s is %s implementation but escalates %v, not %s; the extended scenarios covering %s never fire on the diff most likely to break them",
							path, area.name, escalated, want, area.name)
					}
				}
			}

			for path, why := range area.excluded {
				if !usedExclusion[path] {
					t.Errorf("%s is excluded from the %s area (%s) but is no longer a source file the area matches; drop the exclusion or re-point it", path, area.name, why)
				}
			}
		})
	}
}

func isAreaSource(path string) bool {
	if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
		return false
	}
	return !strings.HasPrefix(path, "tests/")
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

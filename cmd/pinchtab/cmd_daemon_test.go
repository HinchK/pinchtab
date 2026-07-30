package main

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/daemon"
)

type recordingDaemonManager struct {
	calls []string
}

func (m *recordingDaemonManager) record(name string) { m.calls = append(m.calls, name) }

func (m *recordingDaemonManager) Preflight() error { m.record("Preflight"); return nil }

func (m *recordingDaemonManager) Install(configPath string) (string, error) {
	m.record("Install " + configPath)
	return "Installed service", nil
}

func (m *recordingDaemonManager) ServicePath() string { m.record("ServicePath"); return "/tmp/service" }

func (m *recordingDaemonManager) Start() (string, error) {
	m.record("Start")
	return "Pinchtab daemon started.", nil
}

func (m *recordingDaemonManager) Restart() (string, error) {
	m.record("Restart")
	return "Pinchtab daemon restarted.", nil
}

func (m *recordingDaemonManager) Status() (string, error) { m.record("Status"); return "", nil }

func (m *recordingDaemonManager) Stop() (string, error) {
	m.record("Stop")
	return "Pinchtab daemon stopped.", nil
}

func (m *recordingDaemonManager) Uninstall() (string, error) {
	m.record("Uninstall")
	return "Removed service", nil
}

func (m *recordingDaemonManager) ManualInstructions() string {
	m.record("ManualInstructions")
	return ""
}

func (m *recordingDaemonManager) Pid() (string, error) { m.record("Pid"); return "", nil }

func (m *recordingDaemonManager) Logs(n int) (string, error) { m.record("Logs"); return "", nil }

func stubDaemonSeams(t *testing.T, installed bool, statusErr error) (*recordingDaemonManager, *int) {
	t.Helper()

	manager := &recordingDaemonManager{}
	constructed := 0

	origStatus := daemonInstallationStatus
	origManager := daemonCurrentManager
	daemonInstallationStatus = func() (bool, error) { return installed, statusErr }
	daemonCurrentManager = func() (daemon.Manager, error) {
		constructed++
		return manager, nil
	}
	t.Cleanup(func() {
		daemonInstallationStatus = origStatus
		daemonCurrentManager = origManager
	})
	return manager, &constructed
}

func TestDaemonStartAndRestartRefuseWhenNotInstalled(t *testing.T) {
	for _, sub := range []string{"start", "restart"} {
		t.Run(sub, func(t *testing.T) {
			manager, constructed := stubDaemonSeams(t, false, nil)

			var code int
			output := captureStderr(t, func() {
				code = dispatchDaemonCommand(sub, false)
			})

			if code != 1 {
				t.Fatalf("exit code = %d, want 1", code)
			}
			if !strings.Contains(output, "pinchtab daemon install") {
				t.Fatalf("expected remedy in output, got %q", output)
			}
			if !strings.Contains(output, "not installed") {
				t.Fatalf("expected not-installed wording, got %q", output)
			}
			for _, forbidden := range []string{"launchctl", "systemctl", "as root"} {
				if strings.Contains(output, forbidden) {
					t.Fatalf("output leaks %q: %q", forbidden, output)
				}
			}
			if len(manager.calls) != 0 {
				t.Fatalf("manager was asked to act: %v", manager.calls)
			}
			if *constructed != 0 {
				t.Fatalf("manager constructed %d times, want 0", *constructed)
			}
		})
	}
}

func TestDaemonStartRefusesWhenInstallationStateUnknown(t *testing.T) {
	manager, _ := stubDaemonSeams(t, false, errors.New("permission denied"))

	var code int
	output := captureStderr(t, func() {
		code = dispatchDaemonCommand("start", false)
	})

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(output, "cannot determine") || !strings.Contains(output, "permission denied") {
		t.Fatalf("expected cannot-determine wording, got %q", output)
	}
	if strings.Contains(output, "not installed") {
		t.Fatalf("output must not claim not installed: %q", output)
	}
	if len(manager.calls) != 0 {
		t.Fatalf("manager was asked to act: %v", manager.calls)
	}
}

func TestDaemonActionsReachManager(t *testing.T) {
	cases := []struct {
		sub       string
		installed bool
		want      []string
	}{
		{sub: "start", installed: true, want: []string{"Start"}},
		{sub: "restart", installed: true, want: []string{"Restart"}},
		// installed:true on purpose. This case used to read installed:false and expect
		// the manager to be called, which encoded the defect: stop reached the manager
		// with nothing installed and then affirmed it had stopped something.
		{sub: "stop", installed: true, want: []string{"Stop"}},
		{sub: "uninstall", installed: true, want: []string{"Uninstall"}},
	}
	for _, tc := range cases {
		t.Run(tc.sub, func(t *testing.T) {
			manager, _ := stubDaemonSeams(t, tc.installed, nil)

			var code int
			captureStdout(t, func() {
				code = dispatchDaemonCommand(tc.sub, false)
			})

			if code != 0 {
				t.Fatalf("exit code = %d, want 0", code)
			}
			if strings.Join(manager.calls, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("manager calls = %v, want %v", manager.calls, tc.want)
			}
		})
	}
}

func TestDaemonStatusMatchesBareForm(t *testing.T) {
	// An empty HOME keeps both captures off the live service path and log dir:
	// the overview's log tail would otherwise read ~/.pinchtab/logs between the
	// two captures and differ by whatever the running daemon logged in between.
	t.Setenv("HOME", t.TempDir())

	for _, jsonOut := range []bool{false, true} {
		bare := captureStdout(t, func() { dispatchDaemonCommand("", jsonOut) })
		status := captureStdout(t, func() { dispatchDaemonCommand("status", jsonOut) })
		if bare != status {
			t.Fatalf("json=%v: status output differs from bare form\nbare:   %q\nstatus: %q", jsonOut, bare, status)
		}
	}
}

func TestDaemonUsageListsStatusForm(t *testing.T) {
	stubDaemonSeams(t, true, nil)

	var code int
	output := captureStderr(t, func() {
		code = dispatchDaemonCommand("bogus", false)
	})

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(output, "pinchtab daemon <status|install|start|restart|stop|uninstall>") {
		t.Fatalf("expected usage line listing status, got %q", output)
	}
}

func TestPrintDaemonStatusJSONShape(t *testing.T) {
	output := captureStdout(t, func() {
		printDaemonStatusJSON()
	})

	var got map[string]any
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, output)
	}
	for _, key := range []string{"installed", "running"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("expected key %q in JSON output, got %s", key, output)
		}
	}
}

func TestCollectDaemonStatusInvariants(t *testing.T) {
	st := collectDaemonStatus()

	// PID and ServicePath are only meaningful when running/installed; they must
	// stay empty otherwise so the renderers don't print stale rows.
	if !st.Running && st.PID != "" {
		t.Fatalf("PID should be empty when not running, got %q", st.PID)
	}
	if !st.Installed && st.ServicePath != "" {
		t.Fatalf("ServicePath should be empty when not installed, got %q", st.ServicePath)
	}
	// A manager error short-circuits collection before any probing fields are set.
	if st.ManagerError != "" {
		if st.PID != "" || st.ServicePath != "" || st.PreflightError != "" {
			t.Fatalf("expected no probe fields when ManagerError set, got %+v", st)
		}
	}
}

func TestPrintDaemonOverviewIncludesStatusAndHints(t *testing.T) {
	output := captureStdout(t, func() {
		printDaemonOverview()
	})

	for _, needle := range []string{
		"Daemon",
		"service",
		"state",
		"Manage daemon:",
		"pinchtab daemon --json",
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected output to contain %q\n%s", needle, output)
		}
	}
}

// The no-op verbs must not affirm work they did not do. "  [ok] Pinchtab daemon
// stopped." is an affirmative statement, and an operator — or a provisioning script
// reading exit 0 beside it — takes it as confirmation that a service was stopped.
// Exit 0 stays: the manager layer already swallows not-loaded and os.ErrNotExist
// deliberately, and `daemon stop && daemon install && daemon start` never reaches
// install if stop refuses.
func TestDaemonStopAndUninstallReportAnHonestNoOpWhenNotInstalled(t *testing.T) {
	for _, tc := range []struct {
		sub     string
		claim   string
		nothing string
	}{
		{sub: "stop", claim: "stopped", nothing: "nothing to stop"},
		{sub: "uninstall", claim: "uninstalled", nothing: "nothing to uninstall"},
	} {
		t.Run(tc.sub, func(t *testing.T) {
			manager, constructed := stubDaemonSeams(t, false, nil)

			var code int
			output := captureStdout(t, func() {
				code = dispatchDaemonCommand(tc.sub, false)
			})

			if code != 0 {
				t.Fatalf("exit code = %d, want 0 — the contract is idempotent", code)
			}
			if strings.Contains(output, tc.claim) {
				t.Errorf("output claims the service was %s when nothing was installed: %q", tc.claim, output)
			}
			if !strings.Contains(strings.ToLower(output), tc.nothing) {
				t.Errorf("output %q does not say there was %s", output, tc.nothing)
			}
			if !strings.Contains(strings.ToLower(output), "not installed") {
				t.Errorf("output %q does not state the observed fact", output)
			}
			if len(manager.calls) != 0 {
				t.Errorf("manager was asked to act with nothing installed: %v", manager.calls)
			}
			if *constructed != 0 {
				t.Errorf("manager constructed %d times, want 0", *constructed)
			}
		})
	}
}

// Could-not-read is not absence. Rendering an unknown as "nothing to stop" is a false
// statement built from a failed probe, so the no-op verbs must never say it when the
// status query itself failed. The managers already tolerate a missing service, so the
// honest behaviour is to attempt the operation and report what they say.
func TestDaemonStopAndUninstallDoNotClaimNothingToDoWhenTheStateIsUnknown(t *testing.T) {
	for _, tc := range []struct{ sub, want string }{
		{sub: "stop", want: "Stop"},
		{sub: "uninstall", want: "Uninstall"},
	} {
		t.Run(tc.sub, func(t *testing.T) {
			manager, _ := stubDaemonSeams(t, false, errors.New("permission denied"))

			var code int
			output := captureStdout(t, func() {
				code = dispatchDaemonCommand(tc.sub, false)
			})

			if code != 0 {
				t.Fatalf("exit code = %d, want 0", code)
			}
			for _, forbidden := range []string{"nothing to", "not installed"} {
				if strings.Contains(strings.ToLower(output), forbidden) {
					t.Errorf("output %q claims %q from a state that could not be read", output, forbidden)
				}
			}
			if strings.Join(manager.calls, ",") != tc.want {
				t.Errorf("manager calls = %v, want [%s] — an unknown state must be attempted, not assumed absent", manager.calls, tc.want)
			}
		})
	}
}

// The absence assertion: the no-op wording must not become unconditional. With a service
// actually installed, stop still reports the real stopped message from the manager.
func TestDaemonStopStillReportsTheRealResultWhenInstalled(t *testing.T) {
	manager, _ := stubDaemonSeams(t, true, nil)

	var code int
	output := captureStdout(t, func() {
		code = dispatchDaemonCommand("stop", false)
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(output, "Pinchtab daemon stopped.") {
		t.Errorf("output %q is not the manager's real result", output)
	}
	if strings.Contains(strings.ToLower(output), "nothing to stop") {
		t.Errorf("the no-op wording is unconditional: %q", output)
	}
	if strings.Join(manager.calls, ",") != "Stop" {
		t.Errorf("manager calls = %v, want [Stop]", manager.calls)
	}
}

// The guard used to be a two-name allowlist, so every verb that was not start or restart
// inherited "no check" by construction. A verb must now DECLARE its not-installed
// behaviour — which is not the same as being forced to refuse — and one that does not
// declare cannot dispatch at all, so the omission is loud rather than silent.
func TestEveryDispatchedDaemonVerbDeclaresItsNotInstalledPolicy(t *testing.T) {
	raw, err := os.ReadFile("cmd_daemon.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)

	body := src[strings.Index(src, "func dispatchDaemonCommand("):]
	body = body[:strings.Index(body, "\nfunc ")]

	var verbs []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, `case "`) {
			continue
		}
		verbs = append(verbs, strings.TrimSuffix(strings.TrimPrefix(line, `case "`), `":`))
	}
	if len(verbs) == 0 {
		t.Fatal("found no dispatched verbs; this census would pass vacuously")
	}

	for _, verb := range verbs {
		if _, declared := daemonNotInstalledPolicies[verb]; !declared {
			t.Errorf("dispatchDaemonCommand handles %q but it declares no not-installed policy, so it inherits no guard", verb)
		}
	}
	for verb, policy := range daemonNotInstalledPolicies {
		if policy == daemonNoOp && daemonNothingToDo[verb] == "" {
			t.Errorf("%q is a no-op verb with no honest wording, so it would print an empty claim", verb)
		}
		if policy != daemonNoOp && daemonNothingToDo[verb] != "" {
			t.Errorf("%q is not a no-op verb but carries no-op wording", verb)
		}
	}
}

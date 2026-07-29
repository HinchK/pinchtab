package main

import (
	"encoding/json"
	"errors"
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
		{sub: "stop", installed: false, want: []string{"Stop"}},
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

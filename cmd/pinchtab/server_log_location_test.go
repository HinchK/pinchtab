package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func renderAgentHintsString(t *testing.T, st agentStatus) string {
	t.Helper()
	var buf bytes.Buffer
	renderAgentHints(&buf, st)
	return buf.String()
}

// The card's dead end: a server.log last written weeks ago, sitting beside a server
// that has never touched it. Whichever way the server was started, the banner has to
// name the sink that is live and disown the one that is not.
func TestServerLogWhereNamesTheLiveSinkAndDisownsTheStaleFile(t *testing.T) {
	stateDir := "/home/op/.pinchtab"
	backgroundLog := filepath.Join(stateDir, "server.log")
	daemonLog := "/home/op/.pinchtab/logs/daemon.err.log"

	for _, tc := range []struct {
		name            string
		daemonInstalled bool
		backgroundChild bool
		running         bool
		serverLogExists bool
		wantDestination string
		wantStale       string
	}{
		{
			name:            "daemon owns the server, so its log is the live one and server.log is not",
			daemonInstalled: true, running: true, serverLogExists: true,
			wantDestination: daemonLog,
			wantStale:       backgroundLog,
		},
		{
			name:            "background child writes server.log, which is therefore not stale",
			backgroundChild: true, running: true, serverLogExists: true,
			wantDestination: backgroundLog,
			wantStale:       "",
		},
		{
			name:    "foreground server logs to its terminal, so a server.log on disk is a leftover",
			running: true, serverLogExists: true,
			wantDestination: "stdout/stderr of the terminal running `pinchtab server`",
			wantStale:       backgroundLog,
		},
		{
			name:            "nothing running: say so rather than point at a file",
			serverLogExists: true,
			wantDestination: "no server running",
			wantStale:       backgroundLog,
		},
		{
			name:            "no leftover file, nothing to disown",
			running:         true,
			wantDestination: "stdout/stderr of the terminal running `pinchtab server`",
			wantStale:       "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveServerLogWhere(stateDir, daemonLog, tc.daemonInstalled, tc.backgroundChild, tc.running, tc.serverLogExists)
			if got.Destination != tc.wantDestination {
				t.Errorf("destination = %q, want %q", got.Destination, tc.wantDestination)
			}
			if got.StalePath != tc.wantStale {
				t.Errorf("stale path = %q, want %q", got.StalePath, tc.wantStale)
			}
		})
	}
}

// A listener that refuses the token is still a running server. Keying the lookup on
// the strictest state reported "no server running" beside a server that was serving,
// and flagged its live log as stale.
func TestProtectedListenerCountsAsRunningForTheLogLookup(t *testing.T) {
	for _, state := range []healthSnapshotState{
		healthSnapshotRunning,
		healthSnapshotProtected,
		healthSnapshotUnhealthy,
		healthSnapshotInvalid,
	} {
		t.Run(string(state), func(t *testing.T) {
			if state == healthSnapshotStopped {
				t.Fatal("stopped must not be in this list")
			}
			got := resolveServerLogWhere("/home/op/.pinchtab", "", false, true, state != healthSnapshotStopped, true)
			if got.Destination != filepath.Join("/home/op/.pinchtab", "server.log") {
				t.Errorf("state %q resolved to %q, want the background log it is actually writing", state, got.Destination)
			}
			if got.StalePath != "" {
				t.Errorf("state %q flagged its own live log as stale", state)
			}
		})
	}
}

// A banner that resolves the log location and then does not print it leaves the
// operator exactly where the card found them.
func TestBannerPrintsTheLogDestinationAndFlagsAStaleServerLog(t *testing.T) {
	out := renderAgentHintsString(t, agentStatus{
		state:          healthSnapshotRunning,
		running:        true,
		listenAddr:     "127.0.0.1:9867",
		logDestination: "/home/op/.pinchtab/logs/daemon.err.log",
		staleLogPath:   "/home/op/.pinchtab/server.log",
	})

	if !strings.Contains(out, "/home/op/.pinchtab/logs/daemon.err.log") {
		t.Errorf("the banner does not name the live log:\n%s", out)
	}
	if !strings.Contains(out, "logs") {
		t.Errorf("the banner has no logs row:\n%s", out)
	}
	if !strings.Contains(out, "/home/op/.pinchtab/server.log is not being written by this server") {
		t.Errorf("the banner does not disown the stale server.log:\n%s", out)
	}
}

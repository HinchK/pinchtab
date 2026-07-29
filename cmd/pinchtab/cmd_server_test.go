package main

import (
	"encoding/json"
	"log/slog"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/config"
	"github.com/pinchtab/pinchtab/internal/safelog"
)

func TestApplyServerAddressFlagsPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		bind     string
		port     string
		wantBind string
		wantPort string
	}{
		{name: "no flags leaves config", wantBind: "127.0.0.1", wantPort: "9867"},
		{name: "port flag wins", port: "9880", wantBind: "127.0.0.1", wantPort: "9880"},
		{name: "bind flag wins", bind: "0.0.0.0", wantBind: "0.0.0.0", wantPort: "9867"},
		{name: "both flags win", bind: "0.0.0.0", port: "9880", wantBind: "0.0.0.0", wantPort: "9880"},
		{name: "blank flags leave config", bind: "   ", port: "  ", wantBind: "127.0.0.1", wantPort: "9867"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.RuntimeConfig{Bind: "127.0.0.1", Port: "9867"}
			applyServerAddressFlags(cfg, tt.bind, tt.port)
			if cfg.Bind != tt.wantBind {
				t.Errorf("Bind = %q, want %q", cfg.Bind, tt.wantBind)
			}
			if cfg.Port != tt.wantPort {
				t.Errorf("Port = %q, want %q", cfg.Port, tt.wantPort)
			}
		})
	}
}

// The server command must accept the same address overrides as `pinchtab
// bridge`, or the two server modes disagree about how to move off the config
// port — which is what the not-running guidance now tells users to do.
func TestServerCmdAddressFlagsMatchBridge(t *testing.T) {
	for _, name := range []string{"bind", "port"} {
		serverFlag := serverCmd.Flags().Lookup(name)
		if serverFlag == nil {
			t.Fatalf("server has no --%s flag", name)
		}
		bridgeFlag := bridgeCmd.Flags().Lookup(name)
		if bridgeFlag == nil {
			t.Fatalf("bridge has no --%s flag", name)
		}
		if serverFlag.Value.Type() != bridgeFlag.Value.Type() {
			t.Errorf("--%s type = %q, want bridge's %q", name, serverFlag.Value.Type(), bridgeFlag.Value.Type())
		}
		if serverFlag.DefValue != "" {
			t.Errorf("--%s default = %q, want empty so config keeps winning", name, serverFlag.DefValue)
		}
	}
}

// The detached child re-parses its own flags, so an address override applied in
// the parent has to travel with it; otherwise the parent waits on a URL the
// child never binds.
func TestBackgroundServerArgsForwardsAddressFlags(t *testing.T) {
	got := backgroundServerArgs("marker-123", serverBackgroundOptions{
		Bind: "0.0.0.0",
		Port: "9880",
	})
	want := []string{
		"server", "--background-child", "marker-123",
		"--bind", "0.0.0.0",
		"--port", "9880",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("backgroundServerArgs() = %#v, want %#v", got, want)
	}
}

func TestApplyLogLevelResolvesTheRunThreshold(t *testing.T) {
	t.Cleanup(func() { safelog.SetLevel(safelog.DefaultLevel) })

	for _, tc := range []struct {
		name    string
		level   string
		verbose bool
		want    slog.Level
	}{
		{name: "no flags keeps the default", want: safelog.DefaultLevel},
		{name: "verbose means debug", verbose: true, want: slog.LevelDebug},
		{name: "explicit level wins over verbose", level: "error", verbose: true, want: slog.LevelError},
		{name: "explicit warn", level: "warn", want: slog.LevelWarn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			safelog.SetLevel(slog.LevelInfo)
			applyLogLevel(&config.RuntimeConfig{LogLevel: tc.level}, tc.verbose)
			if got := safelog.CurrentLevel(); got != tc.want {
				t.Errorf("level = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDefaultLogLevelRecordsRequestsAndErrors(t *testing.T) {
	if safelog.DefaultLevel > slog.LevelInfo {
		t.Fatalf("DefaultLevel = %v, want info or lower so request lines survive", safelog.DefaultLevel)
	}
}

// A quiet startup banner and recorded errors must be reachable together: the
// banner is a separate field from the level, and neither flag touches the other.
func TestQuietBannerKeepsDefaultLogging(t *testing.T) {
	t.Cleanup(func() { safelog.SetLevel(safelog.DefaultLevel) })
	safelog.SetLevel(slog.LevelError)

	cfg := &config.RuntimeConfig{VerboseBanner: false}
	applyLogLevel(cfg, false)

	if cfg.VerboseBanner {
		t.Errorf("applyLogLevel must not touch the banner")
	}
	if got := safelog.CurrentLevel(); got != safelog.DefaultLevel {
		t.Errorf("level = %v, want the default with a quiet banner", got)
	}
}

// The three-way precedence: --log-level beats server.logLevel, server.logLevel
// beats the default, and -v beats neither. Assigning the flag unconditionally is
// the bug this pins — it erased the configured level on every flagless run.
func TestLogLevelPrecedenceFlagBeatsConfigBeatsDefault(t *testing.T) {
	t.Cleanup(func() { safelog.SetLevel(safelog.DefaultLevel) })

	for _, tc := range []struct {
		name        string
		configLevel string
		flag        string
		verbose     bool
		want        slog.Level
	}{
		{name: "nothing set is the default", want: safelog.DefaultLevel},
		{name: "config alone is honoured", configLevel: "warn", want: slog.LevelWarn},
		{name: "flag beats config", configLevel: "warn", flag: "debug", want: slog.LevelDebug},
		{name: "blank flag leaves config", configLevel: "warn", flag: "   ", want: slog.LevelWarn},
		{name: "verbose loses to config", configLevel: "warn", verbose: true, want: slog.LevelWarn},
		{name: "verbose loses to the flag", flag: "error", verbose: true, want: slog.LevelError},
		{name: "verbose applies with neither", verbose: true, want: slog.LevelDebug},
	} {
		t.Run(tc.name, func(t *testing.T) {
			safelog.SetLevel(slog.LevelInfo)
			cfg := &config.RuntimeConfig{LogLevel: tc.configLevel}
			resolveLogLevel(cfg, tc.flag, tc.verbose)
			if got := safelog.CurrentLevel(); got != tc.want {
				t.Errorf("level = %v, want %v", got, tc.want)
			}
		})
	}
}

// The daemon install and the auto-started server both launch `pinchtab server`
// with no flags, so the config file is the only way either can ask for a level.
// This drives the flagless path end to end: file config in, resolved threshold out.
func TestFlaglessServerResolvesTheConfiguredLogLevel(t *testing.T) {
	t.Cleanup(func() { safelog.SetLevel(safelog.DefaultLevel) })
	safelog.SetLevel(slog.LevelInfo)

	var fc config.FileConfig
	if err := json.Unmarshal([]byte(`{"server":{"logLevel":"warn"}}`), &fc); err != nil {
		t.Fatal(err)
	}
	cfg := &config.RuntimeConfig{}
	config.ApplyFileConfigToRuntime(cfg, &fc)

	resolveLogLevel(cfg, "", false)

	if got := safelog.CurrentLevel(); got != slog.LevelWarn {
		t.Fatalf("flagless server resolved %v, want warn from server.logLevel", got)
	}
}

// If either launcher ever grew a --log-level argument the test above would stop
// standing in for it, and a reader would not know which path is authoritative.
func TestDaemonAndAutoStartLaunchWithoutLogLevelFlags(t *testing.T) {
	if args := autoStartServerArgs("marker-123"); slices.Contains(args, "--log-level") {
		t.Errorf("autoStartServerArgs = %v, now passes --log-level; the config path is no longer the only route", args)
	}
}

// resolveLogLevel is only load-bearing if the command routes through it. A unit
// test on the helper cannot see a Run body that assigns cfg.LogLevel itself, and
// that assignment — `cfg.LogLevel = logLevel`, unconditional — is exactly the bug
// this card fixed. So the source is the assertion: the flag reaches the runtime
// config in one place, inside the helper that knows the precedence.
func TestServerCommandAssignsTheLogLevelOnlyInsideResolveLogLevel(t *testing.T) {
	raw, err := os.ReadFile("cmd_server.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)

	const assignment = "cfg.LogLevel = "
	if got := strings.Count(src, assignment); got != 1 {
		t.Fatalf("cmd_server.go assigns cfg.LogLevel %d times, want exactly 1 (inside resolveLogLevel)", got)
	}
	helper := src[strings.Index(src, "func resolveLogLevel("):]
	if end := strings.Index(helper, "\nfunc "); end >= 0 {
		helper = helper[:end]
	}
	if !strings.Contains(helper, assignment) {
		t.Error("the one cfg.LogLevel assignment is outside resolveLogLevel; the flag can bypass the precedence again")
	}
	if !strings.Contains(src, "resolveLogLevel(cfg, logLevel, verbose)") {
		t.Error("the server command no longer calls resolveLogLevel with the flag and the verbose flag")
	}
}

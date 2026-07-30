package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
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

	const assignment = ".LogLevel = "
	assignments := 0
	scanned := 0
	for _, path := range commandSourceFiles(t) {
		body, err := os.ReadFile(path) // #nosec G304 -- files listed from this package's own directory.
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		if n := strings.Count(string(body), assignment); n > 0 && filepath.Base(path) != "cmd_server.go" {
			t.Errorf("%s assigns %s%d time(s); the level is settled only inside resolveLogLevel, so a command that spawns or becomes a server must pass its flag there instead", filepath.Base(path), assignment, n)
		}
		assignments += strings.Count(string(body), assignment)
	}
	if scanned < 2 {
		t.Fatalf("scanned %d command files; the census matched almost nothing and would pass vacuously", scanned)
	}
	if assignments != 1 {
		t.Fatalf("the command package assigns %s%d times, want exactly 1 (inside resolveLogLevel)", assignment, assignments)
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

// The one-owner rule covers the whole command package, not just the file the
// resolver lives in: cmd_server_ensure.go and cmd_server_background.go hold a
// RuntimeConfig too, so a level assigned there would bypass the precedence
// unseen.
func commandSourceFiles(t *testing.T) []string {
	t.Helper()

	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		out = append(out, path)
	}
	return out
}

// captureRunLog installs the handler a real run installs, at the level a real run
// starts from, so the load-then-resolve sequence below is the production one
// rather than a level set up front.
func captureRunLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(safelog.NewDefaultHandler(&buf)))
	safelog.SetLevel(safelog.DefaultLevel)
	t.Cleanup(func() {
		slog.SetDefault(previous)
		safelog.SetLevel(safelog.DefaultLevel)
	})
	return &buf
}

func writeRunConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PINCHTAB_CONFIG", path)
	t.Setenv("PINCHTAB_TOKEN", "test-token")
	return path
}

// The sequence the defect lived in: the loader describes reading the very file
// that carries server.logLevel, so the diagnostics have to outlive the load and be
// emitted once the level is known.
func loadThenResolve(t *testing.T, flag string, verbose bool) *config.RuntimeConfig {
	t.Helper()
	cfg, diags, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	resolveLogLevel(cfg, flag, verbose)
	config.EmitLoadDiagnostics(diags)
	return cfg
}

func TestConfigFileDebugLevelRecordsTheConfigLoadDiagnostic(t *testing.T) {
	path := writeRunConfig(t, `{"server":{"port":"9867","logLevel":"debug"}}`)
	buf := captureRunLog(t)

	loadThenResolve(t, "", false)

	out := buf.String()
	if !strings.Contains(out, "loading config file") {
		t.Errorf("server.logLevel=debug did not record the config-load diagnostic:\n%s", out)
	}
	if !strings.Contains(out, path) {
		t.Errorf("the diagnostic does not name the file that was read (%s):\n%s", path, out)
	}
}

// Shape 2 of the card: the flag route works too, so --log-level debug can tell you
// which config file it read even when the file itself says nothing about levels.
func TestLogLevelFlagRecordsTheConfigLoadDiagnostic(t *testing.T) {
	path := writeRunConfig(t, `{"server":{"port":"9867"}}`)
	buf := captureRunLog(t)

	loadThenResolve(t, "debug", false)

	if out := buf.String(); !strings.Contains(out, "loading config file") || !strings.Contains(out, path) {
		t.Errorf("--log-level debug did not record the config-load diagnostic with its path:\n%s", out)
	}
}

// A silently rewritten config is the case a user most needs to see.
func TestLegacyBrowserMigrationNoticeIsReachableAtDebug(t *testing.T) {
	writeRunConfig(t, `{"server":{"port":"9867","logLevel":"debug"},"browser":{"binary":"/tmp/chrome"}}`)
	buf := captureRunLog(t)

	cfg := loadThenResolve(t, "", false)

	if !cfg.TargetsSynthesized {
		t.Fatalf("precondition: the legacy browser config was not migrated, so there is no notice to record")
	}
	if out := buf.String(); !strings.Contains(out, "migrated legacy browser config") {
		t.Errorf("the migration notice is unreachable at debug:\n%s", out)
	}
}

// The fix must not promote the diagnostics: a default run stays quiet about which
// file it read, while a warn-level diagnostic from the same load still lands.
func TestDefaultRunRecordsNoConfigLoadDiagnosticButKeepsWarnings(t *testing.T) {
	writeRunConfig(t, `{"server":{"port":"9867"},"browsers":{"default":"not-a-browser"}}`)
	buf := captureRunLog(t)

	loadThenResolve(t, "", false)

	out := buf.String()
	if strings.Contains(out, "loading config file") {
		t.Errorf("a default run recorded the debug config-load diagnostic:\n%s", out)
	}
	if !strings.Contains(out, "not a known browser") {
		t.Errorf("a warn diagnostic from the same load was dropped:\n%s", out)
	}
}

// internal/config must not grow its own precedence: it collects diagnostics and
// emits them on request, and the level decision stays in resolveLogLevel.
func TestConfigPackageHoldsNoLogLevelPrecedence(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "config", "config_load.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"safelog.", "SetLevel("} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("internal/config/config_load.go references %q — the level decision must stay in resolveLogLevel", forbidden)
		}
	}
}

// yoloChildEnv marks the re-executed child that drives serverCmd.Run for real.
const yoloChildEnv = "PINCHTAB_TEST_YOLO_EXIT_CHILD"

// The --yolo branch reports its failures with os.Exit, which no defer and no
// in-process test can observe, so this re-executes the test binary and reads the
// child's own output. Two things have to survive that exit: the loader's warning,
// and the debug line the config asked for — the second one only lands if the level
// is resolved above the --yolo branch rather than merely before the work.
func TestServerYoloExitStillEmitsLoaderDiagnostics(t *testing.T) {
	if os.Getenv(yoloChildEnv) == "1" {
		runYoloExitChild()
		return
	}

	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"configVersion":"` + config.CurrentConfigVersion + `","server":{"token":"test-token","logLevel":"debug"},` +
		`"browser":{"binary":"/tmp/chrome"},"browsers":{"default":"not-a-browser"}}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}

	child := exec.Command(os.Args[0], "-test.run=TestServerYoloExitStillEmitsLoaderDiagnostics", "-test.timeout=60s") // #nosec G204 -- re-executes this test binary.
	child.Env = append(os.Environ(),
		yoloChildEnv+"=1",
		"PINCHTAB_CONFIG="+path,
		"PINCHTAB_TOKEN=test-token",
	)
	raw, err := child.CombinedOutput()
	out := string(raw)

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("child exited with %v, want the --yolo os.Exit(1); output:\n%s", err, out)
	}
	if code := exitErr.ExitCode(); code != 1 {
		t.Fatalf("child exit code = %d, want 1; output:\n%s", code, out)
	}
	if !strings.Contains(out, "--yolo:") {
		t.Fatalf("child did not reach the --yolo failure, so the exit path is unproven:\n%s", out)
	}
	if !strings.Contains(out, "not a known browser") {
		t.Errorf("the --yolo exit discarded the loader warning:\n%s", out)
	}
	if !strings.Contains(out, "loading config file") {
		t.Errorf("server.logLevel=debug was not resolved before the --yolo exit, so its diagnostics stayed hidden:\n%s", out)
	}
	if !strings.Contains(out, "migrated legacy browser config") {
		t.Errorf("the legacy-browser migration notice was lost on the --yolo exit path:\n%s", out)
	}
}

// runYoloExitChild is the child half: production logging, the real Run body, the
// real os.Exit.
func runYoloExitChild() {
	safelog.InstallDefault()
	if err := serverCmd.Flags().Set("yolo", "true"); err != nil {
		fmt.Fprintln(os.Stderr, "child could not set --yolo:", err)
		os.Exit(9)
	}
	serverCmd.Run(serverCmd, nil)
}

// The fix is an ordering, and an ordering is only kept by something that checks
// it: the next flag validation added to either prologue would reopen the window
// silently. Every command that defers its diagnostics must emit them before it can
// leave the prologue, so no exit may sit between the load and the emit.
func TestDeferredDiagnosticsAreEmittedBeforeAnyExit(t *testing.T) {
	const (
		load = "= loadConfigDeferringDiagnostics()"
		emit = "config.EmitLoadDiagnostics("
	)

	// Every call site, not the first per file: a second prologue added to a file
	// that already has one is the likeliest future addition, and stopping at the
	// first match would inspect the old one and pass.
	sites := 0
	for _, path := range commandSourceFiles(t) {
		raw, err := os.ReadFile(path) // #nosec G304 -- files listed from this package's own directory.
		if err != nil {
			t.Fatal(err)
		}
		src := string(raw)

		for offset := 0; ; {
			rel := strings.Index(src[offset:], load)
			if rel < 0 {
				break
			}
			start := offset + rel
			sites++
			where := fmt.Sprintf("%s (prologue at byte %d)", filepath.Base(path), start)
			offset = start + len(load)

			emitAt := strings.Index(src[start:], emit)
			if emitAt < 0 {
				t.Errorf("%s defers its load diagnostics and never emits them", where)
				continue
			}
			window := src[start : start+emitAt]
			for _, escape := range []string{"return", "os.Exit("} {
				if strings.Contains(window, escape) {
					t.Errorf("%s can %s between the load and %s, which discards the loader's diagnostics; move the emit above it:\n%s",
						where, escape, emit, window)
				}
			}
		}
	}
	if sites < 2 {
		t.Fatalf("inspected %d %s call sites; the census matched almost nothing and would pass vacuously", sites, load)
	}
}

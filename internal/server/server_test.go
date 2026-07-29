package server

import (
	"bytes"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/handlers"
	"github.com/pinchtab/pinchtab/internal/safelog"
)

// installCapturedDefault captures the process logger through the very handler
// InstallDefault builds, so these tests observe what a real run records.
func installCapturedDefault(t *testing.T) *bytes.Buffer {
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

func TestDefaultRunRecordsTheRequestLine(t *testing.T) {
	buf := installCapturedDefault(t)

	handler := handlers.LoggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "b5fc54c8642370c7")
		w.WriteHeader(http.StatusNotFound)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/click", nil))

	out := buf.String()
	for _, want := range []string{"msg=request", "requestId=b5fc54c8642370c7", "method=POST", "path=/click", "status=404"} {
		if !strings.Contains(out, want) {
			t.Errorf("a default run dropped %q from the access log:\n%s", want, out)
		}
	}
}

func TestDefaultRunRecordsErrorsAndWarnings(t *testing.T) {
	buf := installCapturedDefault(t)

	slog.Warn("always-on: instance stopped deliberately", "id", "inst_ccb22b39")
	slog.Error("🔥 TARGET CRASHED", "target", "page")

	out := buf.String()
	if !strings.Contains(out, "instance stopped deliberately") {
		t.Errorf("a default run dropped a warning:\n%s", out)
	}
	if !strings.Contains(out, "TARGET CRASHED") {
		t.Errorf("a default run dropped an error:\n%s", out)
	}
}

// RunDashboard starts a server, so no unit test calls it; the second-owner
// regression is only reachable as a structural guard.
func TestOnlySafelogInstallsTheProcessLogger(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	installers := []string{}
	scanned := 0
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scanned++
		rel := filepath.ToSlash(mustRel(t, root, path))
		for _, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			if strings.Contains(trimmed, "slog.SetDefault(") {
				installers = append(installers, rel)
			}
		}
		if strings.HasPrefix(rel, "internal/server/") && strings.Contains(string(raw), "io."+"Discard") {
			t.Errorf("%s discards log output; a run must record its requests and errors without a flag", rel)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	if scanned == 0 {
		t.Fatal("no Go file scanned — this guard is checking nothing")
	}

	want := []string{"internal/safelog/handler.go"}
	if !reflect.DeepEqual(installers, want) {
		t.Errorf("slog.SetDefault callers = %v, want only %v; two writers to the process default logger is what silenced the server", installers, want)
	}
}

func mustRel(t *testing.T, root, path string) string {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatal(err)
	}
	return rel
}

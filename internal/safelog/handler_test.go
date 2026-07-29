package safelog

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestHandlerRedactsAndSanitizesStringAttrs(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewHandler(slog.NewJSONHandler(&buf, nil)))

	logger.Info("hello\x1b[31mworld", "token", "secret-token", "path", "/Users/tester/private.txt\x00")

	out := buf.String()
	if strings.Contains(out, "secret-token") {
		t.Fatalf("expected token to be redacted, got %q", out)
	}
	if strings.Contains(out, "\x1b") {
		t.Fatalf("expected ANSI escapes to be stripped, got %q", out)
	}
	if strings.Contains(out, "\x00") {
		t.Fatalf("expected null bytes to be stripped, got %q", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("expected redacted marker, got %q", out)
	}
}

func TestHandlerTruncatesOversizedStrings(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewHandler(slog.NewTextHandler(&buf, nil)))

	logger.Info("msg", "payload", strings.Repeat("x", MaxStringValueBytes+512))

	out := buf.String()
	if len(out) == 0 {
		t.Fatal("expected log output")
	}
	if strings.Contains(out, strings.Repeat("x", MaxStringValueBytes+128)) {
		t.Fatalf("expected oversized value to be truncated, got %q", out)
	}
}

func TestDefaultLevelRecordsRequestsWarningsAndErrors(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewDefaultHandler(&buf))
	SetLevel(DefaultLevel)
	t.Cleanup(func() { SetLevel(DefaultLevel) })

	logger.Debug("chatty detail")
	logger.Info("request", "requestId", "b5fc54c8642370c7", "status", 200)
	logger.Warn("instance stopped deliberately")
	logger.Error("TARGET CRASHED")

	out := buf.String()
	for _, want := range []string{"msg=request", "requestId=b5fc54c8642370c7", "level=WARN", "level=ERROR", "TARGET CRASHED"} {
		if !strings.Contains(out, want) {
			t.Errorf("default level dropped %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "chatty detail") {
		t.Errorf("default level should not record debug lines:\n%s", out)
	}
}

func TestSetLevelAdjustsTheInstalledHandler(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewDefaultHandler(&buf))
	t.Cleanup(func() { SetLevel(DefaultLevel) })

	SetLevel(slog.LevelError)
	logger.Info("request", "requestId", "abc")
	logger.Error("still recorded")
	if strings.Contains(buf.String(), "msg=request") {
		t.Errorf("an explicit error level should drop request lines:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "still recorded") {
		t.Errorf("errors must survive every level:\n%s", buf.String())
	}

	buf.Reset()
	SetLevel(slog.LevelDebug)
	logger.Debug("chatty detail")
	if !strings.Contains(buf.String(), "chatty detail") {
		t.Errorf("debug level should record debug lines:\n%s", buf.String())
	}
	if got := CurrentLevel(); got != slog.LevelDebug {
		t.Errorf("CurrentLevel() = %v, want debug", got)
	}
}

func TestParseLevel(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		{in: "", want: DefaultLevel},
		{in: "debug", want: slog.LevelDebug},
		{in: "INFO", want: slog.LevelInfo},
		{in: " warn ", want: slog.LevelWarn},
		{in: "warning", want: slog.LevelWarn},
		{in: "error", want: slog.LevelError},
		{in: "silent", want: DefaultLevel, wantErr: true},
	} {
		got, err := ParseLevel(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseLevel(%q) error = %v, wantErr %v", tc.in, err, tc.wantErr)
		}
		if got != tc.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

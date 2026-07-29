package httpx

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/pinchtab/pinchtab/internal/sanitize"
)

const (
	DefaultMaxJSONBodyBytes = 1 << 20
	maxErrorMessageBytes    = 1024
)

type ProblemDetails struct {
	Type      string         `json:"type"`
	Title     string         `json:"title"`
	Status    int            `json:"status"`
	Detail    string         `json:"detail,omitempty"`
	Instance  string         `json:"instance,omitempty"`
	Code      string         `json:"code,omitempty"`
	Retryable bool           `json:"retryable,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

func JSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("json encode", "err", err)
	}
}

func Error(w http.ResponseWriter, code int, err error) {
	message := http.StatusText(code)
	if err != nil {
		message = err.Error()
	}
	if message == "" {
		message = "error"
	}
	ErrorCode(w, code, "error", message, false, nil)
}

func ErrorCode(w http.ResponseWriter, status int, code, message string, retryable bool, details map[string]any) {
	payload := map[string]any{
		"error": SanitizeErrorMessage(message),
		"code":  code,
	}
	if retryable {
		payload["retryable"] = true
	}
	if len(details) > 0 {
		payload["details"] = sanitizeDetails(details)
	}
	JSON(w, status, payload)
}

// sanitizeDetails returns a copy of details with every string cleaned the same
// way as the message beside it. Details carry page-controlled data — dialog
// text, document titles, navigated URLs — and the CLI prints some of them
// straight to a terminal, so leaving them raw would let a visited page smuggle
// ANSI escapes past the sanitizing the message already gets. The input is
// copied rather than rewritten so callers may reuse their map.
func sanitizeDetails(details map[string]any) map[string]any {
	if len(details) == 0 {
		return nil
	}
	out := make(map[string]any, len(details))
	for key, value := range details {
		out[key] = sanitizeDetailValue(value, 0)
	}
	return out
}

const maxDetailDepth = 4

func sanitizeDetailValue(value any, depth int) any {
	if depth > maxDetailDepth {
		return nil
	}
	switch v := value.(type) {
	case string:
		// Not SanitizeErrorMessage: an intentionally empty detail must stay
		// empty rather than becoming the message-level "error" placeholder.
		return sanitize.CleanError(v, maxErrorMessageBytes)
	case []string:
		out := make([]string, len(v))
		for i, s := range v {
			out[i] = sanitize.CleanError(s, maxErrorMessageBytes)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = sanitizeDetailValue(e, depth+1)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			out[k] = sanitizeDetailValue(e, depth+1)
		}
		return out
	default:
		return value
	}
}

func Problem(w http.ResponseWriter, status int, code, detail string, retryable bool, details map[string]any) {
	title := http.StatusText(status)
	if title == "" {
		title = "Error"
	}

	payload := ProblemDetails{
		Type:    "about:blank",
		Title:   title,
		Status:  status,
		Detail:  SanitizeErrorMessage(detail),
		Code:    code,
		Details: sanitizeDetails(details),
	}
	if retryable {
		payload.Retryable = true
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error("problem encode", "err", err)
	}
}

func DecodeJSONBody(w http.ResponseWriter, r *http.Request, maxBytes int64, dst any) error {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxJSONBodyBytes
	}
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes)).Decode(dst)
}

func StatusForJSONDecodeError(err error) int {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

func SanitizeErrorMessage(message string) string {
	message = sanitize.CleanError(message, maxErrorMessageBytes)
	if message == "" {
		return "error"
	}
	return message
}

// CancelOnClientDone cancels the given cancel func when the HTTP client disconnects.
func CancelOnClientDone(reqCtx context.Context, cancel context.CancelFunc) {
	<-reqCtx.Done()
	cancel()
}

// ExtendWriteDeadline pushes the connection's write deadline d into the
// future so a long-running handler (multi-page audit/scrape) is not killed
// by the server's default WriteTimeout before it can write its response.
// Best-effort: an unsupported ResponseWriter keeps the server default.
func ExtendWriteDeadline(w http.ResponseWriter, d time.Duration) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(d))
}

type StatusWriter struct {
	http.ResponseWriter
	Code int
}

func (w *StatusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *StatusWriter) WriteHeader(code int) {
	w.Code = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *StatusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter is not a Hijacker")
}

func (w *StatusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

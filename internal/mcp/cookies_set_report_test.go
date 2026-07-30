package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The WIRING, not the helper: reverting only the call site leaves the table below green, so
// the confirmation has to be asserted through the tool. A 200 carrying set:0 must reach the
// agent as an error result.
func TestCookiesSetToolReportsAnUnstoredCookieAsAnError(t *testing.T) {
	for _, tc := range []struct {
		name     string
		respBody string
		wantErr  bool
	}{
		{name: "stored", respBody: `{"set":1,"failed":0,"total":1}`},
		{
			name:     "not stored",
			respBody: `{"set":0,"failed":1,"total":1,"failures":[{"index":0,"name":"sid","error":"invalid sameSite value"}]}`,
			wantErr:  true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.respBody))
			}))
			defer srv.Close()

			r := callTool(t, "pinchtab_cookies_set", map[string]any{"name": "sid", "value": "abc"}, srv)

			if tc.wantErr {
				if !r.IsError {
					t.Fatalf("tool reported success for %s — the agent cannot tell the cookie was never stored", tc.respBody)
				}
				if text := resultText(t, r); !strings.Contains(text, "invalid sameSite value") {
					t.Errorf("error text = %q, want the reason the browser gave", text)
				}
				return
			}
			if r.IsError {
				t.Fatalf("tool reported an error for a stored cookie: %s", resultText(t, r))
			}
		})
	}
}

// /cookies answers 200 whether or not the browser stored anything, and resultFromBytes only
// treats HTTP >= 400 as an error — so without this check the tool reports a cookie that was
// never set as a success. MCP is the surface where nothing reads the body afterwards, so the
// confirmation has to happen in the handler.
func TestUnsetCookieReportConfirmsTheWrite(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		wantUnse bool
		wantSays string
	}{
		{
			name: "every cookie stored",
			body: `{"set":1,"failed":0,"total":1}`,
		},
		{
			name: "several stored",
			body: `{"set":3,"failed":0,"total":3}`,
		},
		{
			name:     "nothing stored, reason given",
			body:     `{"set":0,"failed":1,"total":1,"failures":[{"index":0,"name":"sid","error":"invalid sameSite value"}]}`,
			wantUnse: true,
			wantSays: "invalid sameSite value",
		},
		{
			name:     "nothing stored, no reason",
			body:     `{"set":0,"failed":1,"total":1}`,
			wantUnse: true,
			wantSays: "no reason given",
		},
		{
			name:     "partially stored",
			body:     `{"set":1,"failed":1,"total":2}`,
			wantUnse: true,
			wantSays: "1 of 2",
		},
		{
			// Fails CLOSED. A response that cannot say how many landed has confirmed
			// nothing, and renaming the field must break the check rather than retire it.
			name:     "counts absent",
			body:     `{"status":"ok"}`,
			wantUnse: true,
			wantSays: "did not report",
		},
		{
			name:     "unreadable",
			body:     `not json`,
			wantUnse: true,
			wantSays: "unreadable",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := unsetCookieReport([]byte(tc.body))

			if !tc.wantUnse {
				if got != "" {
					t.Fatalf("reported %q for a response showing every cookie stored", got)
				}
				return
			}
			if got == "" {
				t.Fatalf("reported success for %s — an unconfirmed write reads as a stored cookie", tc.body)
			}
			if !strings.Contains(got, tc.wantSays) {
				t.Errorf("reason = %q, want it to mention %q so the agent knows what to change", got, tc.wantSays)
			}
		})
	}
}

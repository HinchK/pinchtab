package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/config"
)

func capabilityRefusal(t *testing.T, method, path string, call func(*Handlers, http.ResponseWriter, *http.Request)) (string, string) {
	t.Helper()
	h := New(&mockBridge{}, &config.RuntimeConfig{}, nil, nil, nil)
	w := httptest.NewRecorder()
	call(h, w, httptest.NewRequest(method, path, nil))

	if w.Code != http.StatusForbidden {
		t.Fatalf("%s %s: status = %d, want 403: %s", method, path, w.Code, w.Body.String())
	}
	var resp struct {
		Error   string         `json:"error"`
		Code    string         `json:"code"`
		Details map[string]any `json:"details"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if setting, _ := resp.Details["setting"].(string); setting == "" {
		t.Fatalf("%s %s: refusal dropped details.setting: %v", method, path, resp.Details)
	}
	return resp.Error, resp.Code
}

// /storage is gated by the stateExport capability, so the old wording sent the
// reader looking for a "stateExport endpoint" that does not exist. /cookies is
// the shape whose label already matched, and must still read correctly.
func TestCapabilityRefusalNamesTheCapabilityAndKeepsTheCode(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		call       func(*Handlers, http.ResponseWriter, *http.Request)
		capability string
		setting    string
		wantCode   string
	}{
		{
			name:       "storage is gated by another endpoint's capability",
			method:     "GET",
			path:       "/storage",
			call:       func(h *Handlers, w http.ResponseWriter, r *http.Request) { h.HandleStorage(w, r) },
			capability: "stateExport",
			setting:    "security.allowStateExport",
			wantCode:   "state_export_disabled",
		},
		{
			name:       "cookies label already matched its endpoint",
			method:     "GET",
			path:       "/cookies",
			call:       func(h *Handlers, w http.ResponseWriter, r *http.Request) { h.HandleGetCookies(w, r) },
			capability: "cookies",
			setting:    "security.allowCookies",
			wantCode:   "cookies_disabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message, code := capabilityRefusal(t, tt.method, tt.path, tt.call)

			if code != tt.wantCode {
				t.Fatalf("code = %q, want %q", code, tt.wantCode)
			}
			if strings.Contains(message, tt.capability+" endpoint") {
				t.Fatalf("message calls the capability an endpoint: %q", message)
			}
			if !strings.Contains(message, tt.capability+" capability") {
				t.Fatalf("message does not name the required capability: %q", message)
			}
			if !strings.Contains(message, tt.setting) {
				t.Fatalf("message does not name the setting to change: %q", message)
			}
		})
	}
}

package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDisabledEndpointHandlerIncludesHintAndRemedy(t *testing.T) {
	handler := DisabledEndpointHandler("recording", "security.allowScreencast", "recording_disabled")

	w := httptest.NewRecorder()
	r, _ := http.NewRequest("POST", "/record/start", nil)
	handler(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}

	var resp struct {
		Error   string         `json:"error"`
		Code    string         `json:"code"`
		Details map[string]any `json:"details"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if resp.Code != "recording_disabled" {
		t.Fatalf("code = %q, want recording_disabled", resp.Code)
	}

	hint, _ := resp.Details["hint"].(string)
	remedy, _ := resp.Details["remedy"].(string)

	if hint == "" {
		t.Fatal("expected non-empty hint in details")
	}
	if remedy == "" {
		t.Fatal("expected non-empty remedy in details")
	}
	if remedy != "pinchtab config set security.allowScreencast true" {
		t.Fatalf("remedy = %q, want config set command", remedy)
	}
}

// The label is the capability, not the endpoint: /storage is gated by
// stateExport, and /record/* by the screencast setting. Calling either label an
// "endpoint" sends the reader looking for a route that does not exist.
func TestDisabledEndpointMessageNamesTheCapabilityNotAnEndpoint(t *testing.T) {
	tests := []struct {
		name       string
		capability string
		setting    string
	}{
		{"capability named after another endpoint", "stateExport", "security.allowStateExport"},
		{"capability matching its own endpoint", "cookies", "security.allowCookies"},
		{"feature gated by a differently named setting", "recording", "security.allowScreencast"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := DisabledEndpointMessage(tt.capability, tt.setting)

			if strings.Contains(msg, tt.capability+" endpoint") {
				t.Fatalf("message calls the capability an endpoint: %q", msg)
			}
			if !strings.Contains(msg, tt.capability+" capability") {
				t.Fatalf("message does not name the required capability: %q", msg)
			}
			if !strings.Contains(msg, tt.setting) {
				t.Fatalf("message does not name the setting to change: %q", msg)
			}
		})
	}
}

func TestDisabledEndpointHandlerKeepsSettingHintAndRemedy(t *testing.T) {
	handler := DisabledEndpointHandler("stateExport", "security.allowStateExport", "state_export_disabled")

	w := httptest.NewRecorder()
	r, _ := http.NewRequest("GET", "/storage", nil)
	handler(w, r)

	var resp struct {
		Error   string         `json:"error"`
		Code    string         `json:"code"`
		Details map[string]any `json:"details"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.Code != "state_export_disabled" {
		t.Fatalf("code = %q, want state_export_disabled", resp.Code)
	}
	if strings.Contains(resp.Error, "stateExport endpoint") {
		t.Fatalf("error still describes a stateExport endpoint: %q", resp.Error)
	}
	for key, want := range map[string]string{
		"setting": "security.allowStateExport",
		"hint":    "Enable security.allowStateExport to use this feature.",
		"remedy":  "pinchtab config set security.allowStateExport true",
	} {
		if got, _ := resp.Details[key].(string); got != want {
			t.Fatalf("details[%q] = %q, want %q", key, got, want)
		}
	}
}

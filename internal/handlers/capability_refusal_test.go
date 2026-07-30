package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/config"
	"github.com/pinchtab/pinchtab/internal/routes"
)

// decodeRefusal reads one recorded 403 into its message, code and details. Both
// drives below share it: the endpoint drive, which proves a real handler refuses
// this way, and the writer drive, which sweeps every catalogued capability.
func decodeRefusal(t *testing.T, subject string, w *httptest.ResponseRecorder) (string, string, map[string]any) {
	t.Helper()

	if w.Code != http.StatusForbidden {
		t.Fatalf("%s: status = %d, want 403: %s", subject, w.Code, w.Body.String())
	}
	var resp struct {
		Error   string         `json:"error"`
		Code    string         `json:"code"`
		Details map[string]any `json:"details"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("%s: decode: %v (body=%s)", subject, err, w.Body.String())
	}
	if setting, _ := resp.Details["setting"].(string); setting == "" {
		t.Fatalf("%s: refusal dropped details.setting: %v", subject, resp.Details)
	}
	return resp.Error, resp.Code, resp.Details
}

func capabilityRefusal(t *testing.T, method, path string, call func(*Handlers, http.ResponseWriter, *http.Request)) (string, string, map[string]any) {
	t.Helper()

	h := New(&mockBridge{}, &config.RuntimeConfig{}, nil, nil, nil)
	w := httptest.NewRecorder()
	call(h, w, httptest.NewRequest(method, path, nil))
	return decodeRefusal(t, method+" "+path, w)
}

// remedyFor is the one place the expected remedy is spelled, so every assertion
// below compares against the same contract rather than its own copy.
func remedyFor(setting string) string {
	return "pinchtab config set " + setting + " true && pinchtab server restart"
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
			message, code, _ := capabilityRefusal(t, tt.method, tt.path, tt.call)

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

// The dead end this closes: the remedy stopped at the config write. Writing the
// setting is a successful no-op for the caller — the security block is read at
// boot — so the identical 403 came back, and the agent reading it has no other
// instruction to try.
//
// The scope is derived from the route catalogue rather than listed here, so a
// capability added there is covered the day it is added. Recording and clipboard
// are appended because neither goes through writeCapabilityDisabled — recording
// keeps its own error code, clipboard has no catalogue entry — and those two are
// exactly where the guidance drifted before.
func TestEveryCapabilityRefusalCarriesTheRunnableRemedy(t *testing.T) {
	h := New(&mockBridge{}, &config.RuntimeConfig{}, nil, nil, nil)

	type gate struct {
		name    string
		setting string
		write   func(w *httptest.ResponseRecorder)
	}
	var gates []gate

	caps := routes.CapabilityEndpoints()
	if len(caps) == 0 {
		t.Fatal("the route catalogue reports no capability-gated endpoints; this census would pass vacuously")
	}
	for capability := range caps {
		meta, ok := routes.Meta(capability)
		if !ok {
			t.Errorf("capability %q gates endpoints but has no metadata, so its refusal can name no setting", capability)
			continue
		}
		gates = append(gates, gate{
			name:    string(capability),
			setting: meta.Setting,
			write:   func(w *httptest.ResponseRecorder) { h.writeCapabilityDisabled(w, capability) },
		})
	}

	screencast, _ := routes.Meta(routes.CapScreencast)
	gates = append(gates,
		gate{
			name:    "recording",
			setting: screencast.Setting,
			write: func(w *httptest.ResponseRecorder) {
				h.HandleRecordStart(w, httptest.NewRequest(http.MethodPost, "/record/start", nil))
			},
		},
		gate{
			name:    "clipboard",
			setting: clipboardSetting,
			write:   func(w *httptest.ResponseRecorder) { writeClipboardDisabled(w) },
		},
	)

	for _, g := range gates {
		t.Run(g.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			g.write(w)
			_, _, details := decodeRefusal(t, g.name, w)

			if got, _ := details["setting"].(string); got != g.setting {
				t.Errorf("details.setting = %q, want %q", got, g.setting)
			}
			remedy, _ := details["remedy"].(string)
			if remedy == "" {
				t.Fatalf("%s refusal carries no remedy at all, so the caller is told nothing to try: %v", g.name, details)
			}
			if remedy != remedyFor(g.setting) {
				t.Errorf("remedy = %q, want %q; a gate building its own string drifts from every other capability", remedy, remedyFor(g.setting))
			}
		})
	}
}

// Recording gates on the screencast setting but answers with its own code and
// label. Routing it through the shared builder must not have collapsed that
// distinction — a client matching recording_disabled still has to see it.
func TestRecordingKeepsItsOwnCodeWhileSharingTheRemedy(t *testing.T) {
	message, code, details := capabilityRefusal(t, http.MethodPost, "/record/start",
		func(h *Handlers, w http.ResponseWriter, r *http.Request) { h.HandleRecordStart(w, r) })

	if code != "recording_disabled" {
		t.Errorf("code = %q, want recording_disabled", code)
	}
	if !strings.Contains(message, "recording capability") {
		t.Errorf("message = %q, want it to still name the recording capability", message)
	}
	if got, _ := details["remedy"].(string); got != remedyFor("security.allowScreencast") {
		t.Errorf("remedy = %q, want the shared one", got)
	}
}

// The read and write clipboard gates were byte-identical bodies, which is how one
// of them could have been fixed alone. They now share a writer; this asserts the
// two refusals cannot diverge again.
func TestClipboardReadAndWriteRefusalsAreIdentical(t *testing.T) {
	read, readCode, readDetails := capabilityRefusal(t, http.MethodGet, "/clipboard/read",
		func(h *Handlers, w http.ResponseWriter, r *http.Request) { h.HandleClipboardRead(w, r) })
	write, writeCode, writeDetails := capabilityRefusal(t, http.MethodPost, "/clipboard/write",
		func(h *Handlers, w http.ResponseWriter, r *http.Request) { h.HandleClipboardWrite(w, r) })

	if read != write || readCode != writeCode {
		t.Errorf("clipboard read and write refuse differently:\n read: %q / %q\nwrite: %q / %q", read, readCode, write, writeCode)
	}
	for _, key := range []string{"setting", "hint", "remedy"} {
		if readDetails[key] != writeDetails[key] {
			t.Errorf("clipboard details[%q] differs: read=%v write=%v", key, readDetails[key], writeDetails[key])
		}
	}
	if got, _ := readDetails["remedy"].(string); got != remedyFor(clipboardSetting) {
		t.Errorf("clipboard remedy = %q, want %q", got, remedyFor(clipboardSetting))
	}
}

// Refuse, follow the remedy, retry. The config write and the restart are both
// modelled: SetConfigValue is what `pinchtab config set` performs, and
// ApplyFileConfigToRuntime is what a restart does with the resulting file — the
// running config is built from it at boot and never rebuilt on an edit, which is
// precisely why the remedy has to name the restart. Doing only the first half
// leaves the gate shut, which is the loop the caller was stuck in.
func TestFollowingTheRemedyClearsTheRefusal(t *testing.T) {
	cfg := &config.RuntimeConfig{}
	h := New(&mockBridge{}, cfg, nil, nil, nil)

	w := httptest.NewRecorder()
	h.HandleGetCookies(w, httptest.NewRequest(http.MethodGet, "/cookies", nil))
	_, code, details := decodeRefusal(t, "GET /cookies", w)
	if code != "cookies_disabled" {
		t.Fatalf("code = %q, want cookies_disabled", code)
	}
	setting, _ := details["setting"].(string)
	remedy, _ := details["remedy"].(string)

	fc := config.FileConfig{}
	if err := config.SetConfigValue(&fc, setting, "true"); err != nil {
		t.Fatalf("the remedy's config write fails: %v (remedy=%q)", err, remedy)
	}
	if !strings.Contains(remedy, "pinchtab server restart") {
		t.Fatalf("remedy = %q carries no restart, so the retry below would model a step the caller was never told to take", remedy)
	}
	config.ApplyFileConfigToRuntime(cfg, &fc)

	retry := httptest.NewRecorder()
	h.HandleGetCookies(retry, httptest.NewRequest(http.MethodGet, "/cookies", nil))
	if retry.Code == http.StatusForbidden && strings.Contains(retry.Body.String(), "cookies_disabled") {
		t.Fatalf("after following the remedy the identical refusal came back: %s", retry.Body.String())
	}
}

// The config write ALONE must not clear the gate. If it did, the restart in the
// remedy would be noise and this whole card would be wrong — so the negative is
// what makes the test above evidence for naming the restart rather than against.
func TestTheConfigWriteAloneDoesNotClearTheRefusal(t *testing.T) {
	cfg := &config.RuntimeConfig{}
	h := New(&mockBridge{}, cfg, nil, nil, nil)

	fc := config.FileConfig{}
	if err := config.SetConfigValue(&fc, "security.allowCookies", "true"); err != nil {
		t.Fatalf("config set: %v", err)
	}

	w := httptest.NewRecorder()
	h.HandleGetCookies(w, httptest.NewRequest(http.MethodGet, "/cookies", nil))
	if _, code, _ := decodeRefusal(t, "GET /cookies after config write only", w); code != "cookies_disabled" {
		t.Fatalf("code = %q; the running config picked the edit up without a restart, so the remedy should stop naming one", code)
	}
}

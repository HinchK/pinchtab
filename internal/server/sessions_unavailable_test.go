package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/config"
	"github.com/pinchtab/pinchtab/internal/session"
)

func decodeSessionRefusal(t *testing.T, w *httptest.ResponseRecorder) (code, message, remedy string) {
	t.Helper()

	var resp struct {
		Code    string `json:"code"`
		Error   string `json:"error"`
		Details struct {
			Remedy string `json:"remedy"`
		} `json:"details"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body is not the product's error envelope (%v): %s", err, w.Body.String())
	}
	return resp.Code, resp.Error, resp.Details.Remedy
}

func requestFor(pattern string) *http.Request {
	method, path, _ := strings.Cut(pattern, " ")
	// {id} is a mux wildcard, not a literal; a real caller sends a value.
	return httptest.NewRequest(method, strings.ReplaceAll(path, "{id}", "abc123"), nil)
}

// EVERY pattern must answer, not just POST /sessions: the defect was that an unmounted
// family fell through to net/http's bare "404 page not found", which carries no code at
// all and so cannot be told apart from a typo.
func TestBothUnavailableModesAnswerEveryRouteInTheFamilyWithACode(t *testing.T) {
	for _, mode := range []struct {
		name       string
		register   func(*http.ServeMux)
		wantCode   string
		wantRemedy string
	}{
		{"bridge", RegisterSessionsUnavailableInBridgeMode, CodeSessionsUnavailableInBridgeMode, "pinchtab server"},
		{"sessions disabled", RegisterSessionsDisabled, CodeSessionsDisabled, "sessions.agent.enabled"},
	} {
		t.Run(mode.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mode.register(mux)

			patterns := session.RoutePatterns()
			if len(patterns) == 0 {
				t.Fatal("the shared route list is empty, so this test would pass over nothing")
			}
			for _, pattern := range patterns {
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, requestFor(pattern))

				if w.Code != http.StatusNotFound {
					t.Errorf("%s: status = %d, want 404", pattern, w.Code)
				}
				if strings.Contains(w.Body.String(), "404 page not found") {
					t.Errorf("%s: still the bare mux 404, which carries no code: %s", pattern, w.Body.String())
				}
				code, message, remedy := decodeSessionRefusal(t, w)
				if code != mode.wantCode {
					t.Errorf("%s: code = %q, want %q", pattern, code, mode.wantCode)
				}
				if message == "" {
					t.Errorf("%s: refusal carries no message", pattern)
				}
				if !strings.Contains(remedy, mode.wantRemedy) {
					t.Errorf("%s: remedy = %q, want it to name %q — the remedy that can actually succeed in this mode", pattern, remedy, mode.wantRemedy)
				}
			}
		})
	}
}

// The two modes must not converge: a bridge told to edit config is the original defect,
// and it would be invisible to a test that only checked both answered SOMETHING.
func TestTheTwoUnavailableModesDoNotShareARemedy(t *testing.T) {
	answer := func(register func(*http.ServeMux)) (string, string) {
		mux := http.NewServeMux()
		register(mux)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/sessions", nil))
		code, _, remedy := decodeSessionRefusal(t, w)
		return code, remedy
	}

	bridgeCode, bridgeRemedy := answer(RegisterSessionsUnavailableInBridgeMode)
	disabledCode, disabledRemedy := answer(RegisterSessionsDisabled)

	if bridgeCode == disabledCode {
		t.Errorf("both modes answer with %q, so the caller cannot tell them apart", bridgeCode)
	}
	if bridgeRemedy == disabledRemedy {
		t.Errorf("both modes prescribe %q; one of them cannot work", bridgeRemedy)
	}
	if strings.Contains(bridgeRemedy, "sessions.agent.enabled") {
		t.Errorf("the bridge remedy prescribes a config edit, which is the unescapable loop this fixes: %q", bridgeRemedy)
	}

	for mode, remedy := range map[string]string{"bridge": bridgeRemedy, "disabled": disabledRemedy} {
		if field, prescribed := configSetFieldIn(remedy); prescribed {
			if err := config.SetConfigValue(&config.FileConfig{}, field, "true"); err != nil {
				t.Errorf("the %s remedy says to run `pinchtab config set %s`, and the config editor answers %v — a remedy that cannot run is the dead end this family's codes exist to remove", mode, field, err)
			}
		}
	}
}

// configSetFieldIn reports the field a remedy tells the reader to `pinchtab config set`.
// A remedy naming a key the editor does not know sends the reader round the same loop as
// the bare 404 did, and only the editor can say which keys those are — this family's own
// disabled remedy shipped as `config set sessions.agent.enabled`, which answers "unknown
// field" because the editor knows sessions.dashboard.* and no sessions.agent.* field.
func configSetFieldIn(remedy string) (string, bool) {
	const prefix = "config set "
	at := strings.Index(remedy, prefix)
	if at < 0 {
		return "", false
	}
	field := strings.Fields(remedy[at+len(prefix):])
	if len(field) == 0 {
		return "", false
	}
	return field[0], true
}

// An unknown path must stay a bare 404 saying nothing about sessions — in both modes. This
// is the assertion that stops the fix over-firing onto typos, and the one the review
// predicted nobody would write.
func TestAnUnknownPathIsUntouchedAndSaysNothingAboutSessions(t *testing.T) {
	for _, mode := range []struct {
		name     string
		register func(*http.ServeMux)
	}{
		{"bridge", RegisterSessionsUnavailableInBridgeMode},
		{"sessions disabled", RegisterSessionsDisabled},
	} {
		t.Run(mode.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mode.register(mux)

			// Session-ISH on purpose: a plausible typo is the case that would be
			// swallowed by a prefix-matched registration.
			for _, path := range []string{"/session", "/sessionz", "/sessions/me/extra", "/nope"} {
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))

				if w.Code != http.StatusNotFound {
					t.Errorf("%s: status = %d, want 404", path, w.Code)
				}
				if strings.Contains(strings.ToLower(w.Body.String()), "session") {
					t.Errorf("%s: an unknown path is diagnosed as a session problem it never asked about: %s", path, w.Body.String())
				}
			}
		})
	}
}

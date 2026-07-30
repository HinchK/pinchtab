package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pinchtab/pinchtab/internal/config"
)

// Server mode runs two counter sets in two processes. Moving the bare /metrics to
// the front door must not make the other layer invisible in turn: an instance
// still reports its OWN traffic, and says so.
func TestInstanceMetricsStillReportItsOwnTrafficAndLayer(t *testing.T) {
	resetObservabilityForTests()
	t.Cleanup(resetObservabilityForTests)

	counted := LoggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	for i := 0; i < 3; i++ {
		counted.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/boom", nil))
	}

	h := New(&mockBridge{}, &config.RuntimeConfig{}, nil, nil, nil)
	rec := httptest.NewRecorder()
	h.HandleMetrics(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics = %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}
	if body["layer"] != LayerInstance {
		t.Errorf("layer = %v, want %q", body["layer"], LayerInstance)
	}
	metrics, ok := body["metrics"].(map[string]any)
	if !ok {
		t.Fatalf("metrics block missing: %v", body)
	}
	if got := metrics["requestsTotal"].(float64); got != 3 {
		t.Errorf("requestsTotal = %v, want the 3 requests this process served", got)
	}
	failures, ok := body["failures"].(map[string]any)
	if !ok {
		t.Fatalf("failures block missing: %v", body)
	}
	if got := failures["requestsFailed"].(float64); got != 3 {
		t.Errorf("requestsFailed = %v, want 3", got)
	}
	if failures["layer"] != LayerInstance {
		t.Errorf("failures.layer = %v, want %q", failures["layer"], LayerInstance)
	}
}

// Both modes now serve the same diagnostic keys, which is what makes bridge mode
// usable for reproducing a server-mode problem. A shape that differs per mode is
// the drift this card is closing.
func TestBothModesExposeTheSameDiagnosticKeys(t *testing.T) {
	resetObservabilityForTests()
	t.Cleanup(resetObservabilityForTests)

	frontDoor := DiagnosticsSnapshot(LayerFrontDoor)
	instance := DiagnosticsSnapshot(LayerInstance)

	for key := range frontDoor {
		if _, ok := instance[key]; !ok {
			t.Errorf("instance diagnostics missing %q, which the front door reports", key)
		}
	}
	for key := range instance {
		if _, ok := frontDoor[key]; !ok {
			t.Errorf("front-door diagnostics missing %q, which an instance reports", key)
		}
	}
	if frontDoor["layer"] == instance["layer"] {
		t.Error("both layers report the same label; a client cannot tell the two counter sets apart")
	}
}

// A bridge bug report has to be able to state which build produced it.
func TestBridgeHealthReportsVersion(t *testing.T) {
	h := New(&mockBridge{}, &config.RuntimeConfig{}, nil, nil, nil)
	h.Version = "9.9.9-test"

	rec := httptest.NewRecorder()
	h.HandleHealth(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /health = %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if body["version"] != "9.9.9-test" {
		t.Errorf("version = %v, want the build the bridge is running", body["version"])
	}
}

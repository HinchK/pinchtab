package handlers

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/config"
)

// A drag destination is a selector in the same vocabulary as the source, so it goes through
// the same resolver — refs are what a snapshot hands an agent, and a destination that is
// never resolved is a drag to (0,0) reported as success.
func TestADragDestinationIsResolvedThroughTheSourceResolver(t *testing.T) {
	h := New(&mockBridge{}, &config.RuntimeConfig{}, nil, nil, nil)
	req := bridge.ActionRequest{Kind: bridge.ActionDrag, Selector: "#src", ToSelector: "e9"}

	_, err := h.resolveActionRequestDestination(context.Background(), "tab1", &req)

	if err == nil {
		t.Fatal("a destination selector resolved without any CDP context, so it was not routed through the resolver at all")
	}
	if !strings.Contains(err.Error(), "drag destination") {
		t.Errorf("error = %v, want it to name the end of the drag that failed", err)
	}
	if req.ToNodeID != 0 {
		t.Errorf("ToNodeID = %d, want 0 when resolution failed", req.ToNodeID)
	}
}

// Resolution is idempotent: the tab-scoped handler forwards an already-resolved request to
// the generic one, and re-resolving there would spend the node id it was given.
func TestAnAlreadyResolvedDragDestinationIsLeftAlone(t *testing.T) {
	h := New(&mockBridge{}, &config.RuntimeConfig{}, nil, nil, nil)

	for _, req := range []bridge.ActionRequest{
		{Kind: bridge.ActionDrag, Selector: "#src", ToSelector: "e9", ToNodeID: 77},
		{Kind: bridge.ActionDrag, Selector: "#src"},
		{Kind: bridge.ActionDrag, Selector: "#src", ToX: 400, ToY: 320, HasToXY: true},
	} {
		before := req
		if _, err := h.resolveActionRequestDestination(context.Background(), "tab1", &req); err != nil {
			t.Errorf("%+v: %v", before, err)
		}
		if req.ToNodeID != before.ToNodeID {
			t.Errorf("%+v: ToNodeID became %d", before, req.ToNodeID)
		}
	}
}

// The wiring, not the helper: a drag whose destination never reaches the resolver drags to
// (0,0) and answers dragged:true, which is the silent-success shape this card is about. A
// resolved source (nodeId) is what lets this reach the destination step without a browser.
func TestTheActionHandlerResolvesADragDestination(t *testing.T) {
	h := New(&mockBridge{availableActions: []string{bridge.ActionDrag}}, &config.RuntimeConfig{}, nil, nil, nil)
	body := `{"kind":"drag","nodeId":42,"toSelector":"e9","tabId":"tab1"}`
	w := httptest.NewRecorder()

	h.HandleAction(w, httptest.NewRequest("POST", "/action", strings.NewReader(body)))

	if w.Code == 200 {
		t.Fatalf("the handler answered 200 for a destination it never resolved: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "drag destination") {
		t.Errorf("response = %s, want it to name the drag destination; without that the destination was skipped and the drag went to (0,0)", w.Body.String())
	}
}

// The GET form of /action is a documented convenience, and a destination it drops is the
// same silent no-op in a second place: the drag would run to (0,0) and answer dragged:true.
func TestTheQueryFormOfActionCarriesTheDragDestination(t *testing.T) {
	req, ok := decodeActionRequest(httptest.NewRecorder(), httptest.NewRequest("GET", "/action?kind=drag&selector=%23src&toSelector=e9", nil))
	if !ok {
		t.Fatal("decode refused a drag query")
	}
	if req.ToSelector != "e9" {
		t.Errorf("ToSelector = %q, want e9", req.ToSelector)
	}

	point, ok := decodeActionRequest(httptest.NewRecorder(), httptest.NewRequest("GET", "/action?kind=drag&selector=%23src&toX=400&toY=320", nil))
	if !ok {
		t.Fatal("decode refused a drag query with a coordinate destination")
	}
	if point.ToX != 400 || point.ToY != 320 || !point.HasToXY {
		t.Errorf("destination = (%v,%v) hasToXY=%v, want (400,320) hasToXY=true", point.ToX, point.ToY, point.HasToXY)
	}
}

package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/config"
	"github.com/pinchtab/pinchtab/internal/testbrowser"
)

func TestHandleGetVisible_MissingRef(t *testing.T) {
	h := New(&mockBridge{}, &config.RuntimeConfig{}, nil, nil, nil)
	req := httptest.NewRequest("GET", "/visible", nil)
	w := httptest.NewRecorder()
	h.HandleGetVisible(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "ref") {
		t.Fatalf("expected error about ref, got %s", w.Body.String())
	}
}

func TestHandleGetVisible_NoTab(t *testing.T) {
	h := New(&mockBridge{failTab: true}, &config.RuntimeConfig{}, nil, nil, nil)
	req := httptest.NewRequest("GET", "/visible?ref=e5", nil)
	w := httptest.NewRecorder()
	h.HandleGetVisible(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetVisible_NoSnapshotCache(t *testing.T) {
	mb := &visibleMockBridge{refCache: nil}
	h := New(mb, &config.RuntimeConfig{}, nil, nil, nil)
	req := httptest.NewRequest("GET", "/visible?ref=e5", nil)
	w := httptest.NewRecorder()
	h.HandleGetVisible(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "not found") {
		t.Fatalf("expected not-found error, got %s", w.Body.String())
	}
}

func TestHandleGetVisible_RefNotFound(t *testing.T) {
	mb := &visibleMockBridge{
		refCache: &bridge.RefCache{
			Refs:    map[string]int64{"e1": 100},
			Targets: map[string]bridge.RefTarget{"e1": {BackendNodeID: 100}},
		},
	}
	h := New(mb, &config.RuntimeConfig{}, nil, nil, nil)
	req := httptest.NewRequest("GET", "/visible?ref=e99", nil)
	w := httptest.NewRecorder()
	h.HandleGetVisible(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "e99") {
		t.Fatalf("expected not-found error mentioning e99, got %s", w.Body.String())
	}
}

func TestHandleGetVisible_SelectorParamEquivalentToRef(t *testing.T) {
	newH := func() *Handlers {
		mb := &visibleMockBridge{
			refCache: &bridge.RefCache{
				Refs:    map[string]int64{"e1": 100},
				Targets: map[string]bridge.RefTarget{"e1": {BackendNodeID: 100}},
			},
		}
		return New(mb, &config.RuntimeConfig{}, nil, nil, nil)
	}

	// Both the unified-selector `selector` param and the legacy `ref` alias must
	// feed the same resolver: an unknown ref via either param yields the same 404.
	for _, q := range []string{"selector=e99", "ref=e99"} {
		req := httptest.NewRequest("GET", "/visible?"+q, nil)
		w := httptest.NewRecorder()
		newH().HandleGetVisible(w, req)
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "e99") {
			t.Fatalf("%s: expected 404 mentioning e99, got %d: %s", q, w.Code, w.Body.String())
		}
	}

	// `selector` takes precedence over `ref` when both are present.
	req := httptest.NewRequest("GET", "/visible?selector=e99&ref=e1", nil)
	w := httptest.NewRecorder()
	newH().HandleGetVisible(w, req)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "e99") {
		t.Fatalf("expected selector precedence (404 mentioning e99), got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleTabGetVisible_MissingTabID(t *testing.T) {
	h := New(&mockBridge{}, &config.RuntimeConfig{}, nil, nil, nil)
	req := httptest.NewRequest("GET", "/tabs//visible?ref=e5", nil)
	w := httptest.NewRecorder()
	h.HandleTabGetVisible(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleTabGetVisible_ForwardsTabID(t *testing.T) {
	h := New(&mockBridge{failTab: true}, &config.RuntimeConfig{}, nil, nil, nil)
	req := httptest.NewRequest("GET", "/tabs/tab_abc/visible?ref=e5", nil)
	req.SetPathValue("id", "tab_abc")
	w := httptest.NewRecorder()
	h.HandleTabGetVisible(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVisibleRoutesRegistered(t *testing.T) {
	h := New(&mockBridge{failTab: true}, &config.RuntimeConfig{}, nil, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, nil)

	paths := []string{"/visible?ref=e1", "/tabs/tab1/visible?ref=e1"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code == http.StatusNotFound && strings.Contains(w.Body.String(), "404 page not found") {
				t.Fatalf("route %s not registered", path)
			}
		})
	}
}

// visibleMockBridge extends mockBridge with GetRefCache support.
type visibleMockBridge struct {
	mockBridge
	refCache *bridge.RefCache
}

func (m *visibleMockBridge) GetRefCache(tabID string) *bridge.RefCache {
	return m.refCache
}

const visibleFixtureHTML = `<style>
#fixedByStylesheet { position: fixed; top:10px; left:10px; width:120px; height:40px }
#stickyByStylesheet { position: sticky; top:0; width:120px; height:40px }
#opaqueByStylesheet { position: fixed; top:200px; left:10px; width:120px; height:40px; opacity: 0 }
</style>
<div id="fixedByStylesheet">fixed by stylesheet</div>
<div id="fixedInline" style="position:fixed; top:80px; left:10px; width:120px; height:40px">fixed inline</div>
<div id="stickyByStylesheet">sticky by stylesheet</div>
<div id="stickyInline" style="position:sticky; top:0; width:120px; height:40px">sticky inline</div>
<div id="normalFlow" style="width:120px; height:40px">normal flow</div>
<div id="opaqueByStylesheet">fixed but transparent</div>
<div id="displayNone" style="display:none">display none</div>
<div id="visibilityHidden" style="visibility:hidden; width:120px; height:40px">visibility hidden</div>
<div id="opacityZero" style="opacity:0; width:120px; height:40px">opacity zero</div>
<div id="zeroSize" style="width:0; height:0">zero size</div>`

func runElementVisibleJS(t *testing.T, ctx context.Context, selector string) bool {
	t.Helper()

	var nodes []*cdp.Node
	if err := chromedp.Run(ctx, chromedp.Nodes(selector, &nodes, chromedp.ByQuery)); err != nil {
		t.Fatalf("%s: %v", selector, err)
	}
	if len(nodes) == 0 {
		t.Fatalf("%s matched nothing in the fixture", selector)
	}

	var visible bool
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		object, err := dom.ResolveNode().WithBackendNodeID(nodes[0].BackendNodeID).Do(ctx)
		if err != nil {
			return err
		}
		result, exception, err := runtime.CallFunctionOn(elementVisibleJS).WithObjectID(object.ObjectID).WithReturnByValue(true).Do(ctx)
		if err != nil {
			return err
		}
		if exception != nil {
			return exception
		}
		return json.Unmarshal(result.Value, &visible)
	})); err != nil {
		t.Fatalf("%s: %v", selector, err)
	}
	return visible
}

func TestTheVisibleEndpointReadsThePositionCSSComputed(t *testing.T) {
	chromePath := testbrowser.Path(t)

	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.UserDataDir(testbrowser.ProfileDir(t)),
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
	)...)
	ctx, cancelBrowser := chromedp.NewContext(alloc)
	ctx, cancelTimeout := context.WithTimeout(ctx, 30*time.Second)
	t.Cleanup(func() {
		cancelTimeout()
		cancelBrowser()
		cancelAlloc()
	})

	url := "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(visibleFixtureHTML))
	if err := chromedp.Run(ctx, chromedp.Navigate(url)); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		selector string
		want     bool
		why      string
	}{
		{selector: "#fixedByStylesheet", want: true, why: "fixed by a stylesheet rule: the case that reported not visible while on screen"},
		{selector: "#fixedInline", want: true, why: "fixed by the style attribute: what worked before, and must not regress"},
		{selector: "#stickyByStylesheet", want: true, why: "sticky by a stylesheet rule"},
		{selector: "#stickyInline", want: true, why: "sticky by the style attribute"},
		{selector: "#normalFlow", want: true, why: "an ordinary in-flow element never needed the positioned branch"},
		{selector: "#displayNone", want: false, why: "display:none"},
		{selector: "#visibilityHidden", want: false, why: "visibility:hidden"},
		{selector: "#opacityZero", want: false, why: "opacity:0"},
		{selector: "#opaqueByStylesheet", want: false, why: "fixed and positioned, but transparent: reaching the positioned branch must not skip the hidden checks"},
		{selector: "#zeroSize", want: false, why: "zero-size box"},
	} {
		if got := runElementVisibleJS(t, ctx, tc.selector); got != tc.want {
			t.Errorf("%s reported visible=%v, want %v — %s", tc.selector, got, tc.want, tc.why)
		}
	}
}

// The browser-backed table above does not run where no browser is installed.
func TestTheVisiblePredicateNeverReadsTheInlineStyleAttribute(t *testing.T) {
	if strings.Contains(elementVisibleJS, "el.style.") {
		t.Errorf("the visible predicate reads the inline style attribute:\n%s", elementVisibleJS)
	}
	if !strings.Contains(elementVisibleJS, "style.position") {
		t.Errorf("the visible predicate no longer consults a position at all, so a fixed element falls to offsetParent alone:\n%s", elementVisibleJS)
	}
	if got := strings.Count(elementVisibleJS, "getComputedStyle"); got != 1 {
		t.Errorf("getComputedStyle appears %d times, want exactly 1; the position must come from the call the predicate already makes:\n%s", got, elementVisibleJS)
	}
	if strings.Index(elementVisibleJS, "getComputedStyle") > strings.Index(elementVisibleJS, "style.position") {
		t.Errorf("style.position is read before getComputedStyle assigns it:\n%s", elementVisibleJS)
	}
}

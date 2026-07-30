package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/config"
	"github.com/pinchtab/pinchtab/internal/testbrowser"
	"github.com/pinchtab/semantic"
	"github.com/pinchtab/semantic/recovery"
)

// orderFixture serves a form whose button posts an order, and counts the posts. The counter
// is the instrument this card turns on: a response saying the ref was not found is only
// wrong if an order was placed anyway, and no assertion on the response text can show that.
type orderFixture struct {
	server *httptest.Server
	orders *int64
}

func newOrderFixture(t *testing.T) orderFixture {
	t.Helper()
	var orders int64
	mux := http.NewServeMux()
	mux.HandleFunc("/order", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			atomic.AddInt64(&orders, 1)
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<h1>Order placed</h1>`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<h1>Checkout</h1>
			<form action="/order" method="post"><button id="b">Place order</button></form>`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return orderFixture{server: srv, orders: &orders}
}

func (f orderFixture) placed() int64 { return atomic.LoadInt64(f.orders) }

// settleForOrder waits for a post that may still be in flight. A form submission is
// dispatched by the browser and arrives at the server after the call returns, so reading
// the counter immediately would let a placed order pass as none — the exact failure this
// fixture exists to detect.
func (f orderFixture) settleForOrder() int64 {
	for i := 0; i < 40 && f.placed() == 0; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	return f.placed()
}

// staleSubmitHandlers stands up a real browser on the checkout page with handlers whose
// recovery engine is the production one, and records the intent a snapshot would have
// cached for the button's ref. That intent is what recovery matches on, so this reproduces
// the state the card describes: a ref the cache no longer resolves, whose remembered
// descriptor still names the submit button.
func staleSubmitHandlers(t *testing.T, f orderFixture) (*Handlers, context.Context, string) {
	t.Helper()
	chromePath := testbrowser.Path(t)
	profile := testbrowser.ProfileDir(t)
	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.UserDataDir(profile),
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

	if err := chromedp.Run(ctx,
		chromedp.Navigate(f.server.URL+"/"),
		chromedp.WaitVisible("#b", chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}

	cfg := &config.RuntimeConfig{ActionTimeout: 10 * time.Second, DefaultBrowser: config.BrowserChrome, StateDir: t.TempDir()}
	b := bridge.New(context.Background(), ctx, cfg)
	const tabID = "tab-stale-submit"
	b.RegisterTab(tabID, ctx)
	h := New(b, cfg, nil, nil, nil)

	h.Recovery.RecordIntent(tabID, "e2", recovery.IntentEntry{
		Descriptor: semantic.ElementDescriptor{Ref: "e2", Role: "button", Name: "Place order"},
		CachedAt:   time.Now(),
	})
	return h, ctx, tabID
}

func staleClick() *bridge.ActionRequest {
	return &bridge.ActionRequest{Kind: bridge.ActionClick, Ref: "e2"}
}

// The defect: the click was DISPATCHED at a node recovery matched, the form posted, and the
// caller was told the ref could not be found — so the only sane reaction, retry after a
// re-snapshot, places a second order. The refusal has to come before the dispatch, which is
// why the counter rather than the message is the assertion.
func TestAStaleRefOnASubmitControlIsRefusedBeforeAnyOrderIsPlaced(t *testing.T) {
	f := newOrderFixture(t)
	h, ctx, tabID := staleSubmitHandlers(t, f)

	_, _, rr, err := h.executeActionResilient(ctx, staleClick(), h.Config, tabID, true)

	if !errors.Is(err, ErrStaleSubmitTarget) {
		t.Fatalf("error = %v, want the stale-submit refusal", err)
	}
	if placed := f.settleForOrder(); placed != 0 {
		t.Errorf("%d order(s) placed by a call that refused; the refusal must precede the dispatch", placed)
	}
	if rr != nil {
		t.Errorf("a recovery block was published for a call that never re-resolved anything: %+v", rr)
	}
	if got := err.Error(); !strings.Contains(got, "/snapshot") {
		t.Errorf("refusal %q does not tell the caller to re-snapshot", got)
	}
}

// A ref that is stale on a page with nothing to submit keeps the ordinary recovery path:
// this refusal is scoped to submit controls, not to stale refs at large.
func TestAStaleRefWithNoSubmitControlIsNotRefusedByTheSubmitGuard(t *testing.T) {
	f := newOrderFixture(t)
	h, ctx, tabID := staleSubmitHandlers(t, f)

	if err := chromedp.Run(ctx, chromedp.Navigate(f.server.URL+"/order")); err != nil {
		t.Fatal(err)
	}

	_, _, _, err := h.executeActionResilient(ctx, staleClick(), h.Config, tabID, true)

	if errors.Is(err, ErrStaleSubmitTarget) {
		t.Errorf("a page with no form answered the submit refusal: %v", err)
	}
	if placed := f.settleForOrder(); placed != 0 {
		t.Errorf("%d order(s) placed on a page with no form", placed)
	}
}

// The guard is what stops the order, not the wording of the refusal: with the DOM check
// stubbed to the answer it gave before this card — no, this is not a submit — the identical
// call dispatches at the node recovery MATCHED and the form posts. That is the reported
// defect reproduced in the suite, so the refusal above cannot be weakened without this
// turning red.
func TestTheSubmitCheckIsWhatStopsTheOrder(t *testing.T) {
	f := newOrderFixture(t)
	h, ctx, tabID := staleSubmitHandlers(t, f)

	original := submitControlJS
	t.Cleanup(func() { submitControlJS = original })
	submitControlJS = `function() { return false; }`

	_, _, _, err := h.executeActionResilient(ctx, staleClick(), h.Config, tabID, true)

	if errors.Is(err, ErrStaleSubmitTarget) {
		t.Fatalf("the stubbed check still refused, so the refusal is not coming from the DOM check: %v", err)
	}
	if placed := f.settleForOrder(); placed != 1 {
		t.Fatalf("orders placed without the check = %d, want 1: recovery no longer reaches the button, so this fixture has stopped reproducing the defect the guard prevents", placed)
	}
}

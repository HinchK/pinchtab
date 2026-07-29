package bridge

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/pinchtab/pinchtab/internal/selector"
	"github.com/pinchtab/pinchtab/internal/testbrowser"
)

// stubScope records what the shared recursion asks of a scope, so the grammar
// can be exercised without a browser. The real scopes differ only in these two
// answers, which is the premise the shared recursion rests on.
type stubScope struct {
	atCalls  []stubAt
	refCalls []string
	nodeID   int64
	refErr   error
}

type stubAt struct {
	kind    selector.Kind
	value   string
	index   int
	fromEnd bool
}

func (s *stubScope) resolveAt(_ context.Context, sel selector.Selector, index int, fromEnd bool) (int64, error) {
	s.atCalls = append(s.atCalls, stubAt{sel.Kind, sel.Value, index, fromEnd})
	return s.nodeID, nil
}

func (s *stubScope) resolveRef(_ context.Context, sel selector.Selector, _ *RefCache) (int64, error) {
	s.refCalls = append(s.refCalls, sel.Value)
	if s.refErr != nil {
		return 0, s.refErr
	}
	return s.nodeID, nil
}

// Every wrapper form must reach the leaf with the same index/fromEnd it did when
// each scope had its own copy of this grammar.
func TestResolveWrapperUnwrapsToTheSameLeafForEveryForm(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want stubAt
	}{
		{"first:css:#a", stubAt{selector.KindCSS, "#a", 0, false}},
		{"last:css:#a", stubAt{selector.KindCSS, "#a", 0, true}},
		{"nth:2:css:#a", stubAt{selector.KindCSS, "#a", 2, false}},
		{"first:text:Save", stubAt{selector.KindText, "Save", 0, false}},
		// Nesting collapses: the innermost wrapper decides.
		{"first:last:css:#a", stubAt{selector.KindCSS, "#a", 0, true}},
		{"nth:3:first:css:#a", stubAt{selector.KindCSS, "#a", 0, false}},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			scope := &stubScope{nodeID: 42}
			sel := selector.Parse(tc.raw)

			got, err := resolveWrapper(context.Background(), scope, sel, nil)
			if err != nil {
				t.Fatalf("resolveWrapper(%q): %v", tc.raw, err)
			}
			if got != 42 {
				t.Errorf("node id = %d, want 42", got)
			}
			if len(scope.atCalls) != 1 || scope.atCalls[0] != tc.want {
				t.Errorf("leaf calls = %+v, want exactly one %+v", scope.atCalls, tc.want)
			}
		})
	}
}

// A wrapped ref must go through the scope's own resolveRef — that is where the
// dialog containment check lives, so routing it anywhere else would let a
// dialog-scoped action reach outside the dialog.
func TestResolveWrapperRoutesRefsThroughTheScope(t *testing.T) {
	outside := errors.New("outside")
	scope := &stubScope{nodeID: 7, refErr: outside}

	_, err := resolveWrapper(context.Background(), scope, selector.Parse("first:ref:e0"), nil)

	if !errors.Is(err, outside) {
		t.Fatalf("err = %v, want the scope's own ref error", err)
	}
	if len(scope.refCalls) != 1 || scope.refCalls[0] != "e0" {
		t.Errorf("ref calls = %v, want exactly [e0]", scope.refCalls)
	}
	if len(scope.atCalls) != 0 {
		t.Errorf("a ref reached the leaf resolver: %+v", scope.atCalls)
	}
}

// last:/nth: cannot select among a single cached ref, and semantic selectors
// belong to the handler layer. Both rejections predate the shared recursion.
func TestResolveWrapperRejectionsUnchanged(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{"last:ref:e0", "ref selector cannot be used with last/nth"},
		{"nth:1:ref:e0", "ref selector cannot be used with last/nth"},
		{"first:find:Save", "semantic selectors must be resolved at the handler layer via /find"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			scope := &stubScope{nodeID: 7}

			_, err := resolveWrapper(context.Background(), scope, selector.Parse(tc.raw), nil)

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if len(scope.atCalls) != 0 || len(scope.refCalls) != 0 {
				t.Errorf("rejected selector still reached the scope: at=%+v ref=%v", scope.atCalls, scope.refCalls)
			}
		})
	}
}

// The two scopes are deliberately NOT interchangeable on refs. frameScope hands
// back whatever the cache holds; nodeScope proves containment first. This pins
// the half that needs no browser — the containment half is pinned against a real
// dialog in the modal test.
func TestScopeRefBehaviourDiffersByDesign(t *testing.T) {
	cache := &RefCache{Targets: map[string]RefTarget{"e0": {BackendNodeID: 99}}}
	sel := selector.Parse("ref:e0")

	got, err := (frameScope{}).resolveRef(context.Background(), sel, cache)
	if err != nil || got != 99 {
		t.Fatalf("frame scope ref = (%d, %v), want (99, nil)", got, err)
	}

	if _, err := (frameScope{}).resolveRef(context.Background(), sel, nil); !errors.Is(err, ErrSelectorNoMatch) {
		t.Errorf("frame scope with no cache = %v, want ErrSelectorNoMatch", err)
	}
	if _, err := (nodeScope{backendNodeID: 1}).resolveRef(context.Background(), sel, nil); !errors.Is(err, ErrSelectorNoMatch) {
		t.Errorf("node scope with no cache = %v, want ErrSelectorNoMatch", err)
	}

	// Divergence between the former copies, preserved rather than reconciled:
	// a cached ref carrying a zero backend node id is a successful resolution to
	// 0 in the frame scope and a not-in-cache error in the node scope.
	zero := &RefCache{Targets: map[string]RefTarget{"e0": {BackendNodeID: 0}}}
	if got, err := (frameScope{}).resolveRef(context.Background(), sel, zero); err != nil || got != 0 {
		t.Errorf("frame scope zero-id ref = (%d, %v), want (0, nil) as before", got, err)
	}
	if _, err := (nodeScope{backendNodeID: 1}).resolveRef(context.Background(), sel, zero); !errors.Is(err, ErrSelectorNoMatch) {
		t.Errorf("node scope zero-id ref = %v, want ErrSelectorNoMatch as before", err)
	}
}

// The stub table above proves what the recursion asks of a scope, but both real
// scopes answer in the same shape, so it is blind to WHICH scope a wrapper form
// is given. That is the one mistake this refactor made cheap: the scope is now a
// value at two call sites, so handing the node entry a frameScope is a one-token
// edit. This is the browser-backed guard for it — the dialog twin must win over
// the background twins that bracket it in document order, which is exactly what
// a frame-rooted search would return instead.
func TestDialogScopedWrapperFormsStayInsideTheScope(t *testing.T) {
	ctx := newScopedWrapperFixture(t)

	modalNodeID, open, err := TopmostModalNodeID(ctx, "")
	if err != nil || !open {
		t.Fatalf("topmost dialog = (%d, %v, %v), want a visible dialog", modalNodeID, open, err)
	}

	for _, tc := range []struct {
		selector string
		wantID   string
	}{
		{selector: "first:text:Save", wantID: "dialog-first"},
		{selector: "last:text:Save", wantID: "dialog-last"},
		{selector: "nth:1:text:Save", wantID: "dialog-last"},
		{selector: "first:css:button", wantID: "dialog-first"},
		{selector: "last:css:button", wantID: "dialog-last"},
	} {
		t.Run(tc.selector, func(t *testing.T) {
			nodeID, err := ResolveUnifiedSelectorWithinNode(ctx, selector.Parse(tc.selector), nil, modalNodeID)
			if err != nil {
				t.Fatalf("resolve %s within the dialog: %v", tc.selector, err)
			}
			var gotID string
			if err := callFunctionOnNodeForTest(ctx, nodeID, `function() { return this.id; }`, &gotID); err != nil {
				t.Fatal(err)
			}
			if gotID != tc.wantID {
				t.Errorf("%s resolved %q, want %q — the wrapper form escaped the dialog scope", tc.selector, gotID, tc.wantID)
			}
		})
	}
}

// Background twins bracket the dialog so that first: and last: each discriminate:
// a frame-rooted first: returns background-first and a frame-rooted last: returns
// background-last, while both correct answers are inside the dialog.
func newScopedWrapperFixture(t *testing.T) context.Context {
	t.Helper()
	chromePath := testbrowser.Path(t)
	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.UserDataDir(t.TempDir()),
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
	)...)
	ctx, cancelBrowser := chromedp.NewContext(alloc)
	ctx, cancelTimeout := context.WithTimeout(ctx, 20*time.Second)
	t.Cleanup(func() {
		cancelTimeout()
		cancelBrowser()
		cancelAlloc()
	})

	html := `<style>[role=dialog] { position: fixed; inset: 20px; background: white; }</style>
	<button id="background-first">Save</button>
	<div id="dlg" role="dialog" aria-modal="true">
		<button id="dialog-first">Save</button>
		<button id="dialog-last">Save</button>
	</div>
	<button id="background-last">Save</button>`
	dataURL := "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(html))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(dataURL),
		chromedp.WaitVisible("#dialog-first", chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	return ctx
}

package bridge

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/selector"
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

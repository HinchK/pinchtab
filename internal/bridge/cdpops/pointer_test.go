package cdpops

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/input"
)

func TestDispatchMouseMoveFallsBackToSyntheticOnDeadline(t *testing.T) {
	origReal := dispatchRealMouseMoveFunc
	origSynthetic := dispatchSyntheticMouseMoveFunc
	t.Cleanup(func() {
		dispatchRealMouseMoveFunc = origReal
		dispatchSyntheticMouseMoveFunc = origSynthetic
	})

	dispatchRealMouseMoveFunc = func(context.Context, float64, float64, input.MouseButton, int64) error {
		return context.DeadlineExceeded
	}

	called := false
	dispatchSyntheticMouseMoveFunc = func(_ context.Context, x, y float64, button input.MouseButton, buttons int64) error {
		called = true
		if x != 12 || y != 34 {
			t.Fatalf("synthetic move coordinates = (%v, %v), want (12, 34)", x, y)
		}
		if button != input.Left || buttons != 1 {
			t.Fatalf("synthetic move button state = (%v, %d), want (%v, 1)", button, buttons, input.Left)
		}
		return nil
	}

	if err := dispatchMouseMove(context.Background(), 12, 34, input.Left, 1); err != nil {
		t.Fatalf("dispatchMouseMove returned error: %v", err)
	}
	if !called {
		t.Fatal("expected synthetic fallback to run")
	}
}

func TestDispatchMouseMoveDoesNotFallbackOnNonDeadlineError(t *testing.T) {
	origReal := dispatchRealMouseMoveFunc
	origSynthetic := dispatchSyntheticMouseMoveFunc
	t.Cleanup(func() {
		dispatchRealMouseMoveFunc = origReal
		dispatchSyntheticMouseMoveFunc = origSynthetic
	})

	want := errors.New("cdp failed")
	dispatchRealMouseMoveFunc = func(context.Context, float64, float64, input.MouseButton, int64) error {
		return want
	}
	dispatchSyntheticMouseMoveFunc = func(context.Context, float64, float64, input.MouseButton, int64) error {
		t.Fatal("synthetic fallback should not run for non-timeout CDP errors")
		return nil
	}

	if err := dispatchMouseMove(context.Background(), 12, 34, input.None, 0); !errors.Is(err, want) {
		t.Fatalf("dispatchMouseMove error = %v, want %v", err, want)
	}
}

func TestDispatchMouseMoveContextCancellationWinsOverFallback(t *testing.T) {
	origReal := dispatchRealMouseMoveFunc
	origSynthetic := dispatchSyntheticMouseMoveFunc
	t.Cleanup(func() {
		dispatchRealMouseMoveFunc = origReal
		dispatchSyntheticMouseMoveFunc = origSynthetic
	})

	dispatchRealMouseMoveFunc = func(context.Context, float64, float64, input.MouseButton, int64) error {
		return context.DeadlineExceeded
	}
	dispatchSyntheticMouseMoveFunc = func(context.Context, float64, float64, input.MouseButton, int64) error {
		t.Fatal("synthetic fallback should not run after caller context cancellation")
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := dispatchMouseMove(ctx, 12, 34, input.None, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("dispatchMouseMove error = %v, want context.Canceled", err)
	}
}

func TestDispatchMouseMoveToNodeFallsBackToSyntheticNodeMove(t *testing.T) {
	origReal := dispatchRealMouseMoveFunc
	origSyntheticNode := dispatchSyntheticMouseMoveOnNodeFunc
	t.Cleanup(func() {
		dispatchRealMouseMoveFunc = origReal
		dispatchSyntheticMouseMoveOnNodeFunc = origSyntheticNode
	})

	dispatchRealMouseMoveFunc = func(context.Context, float64, float64, input.MouseButton, int64) error {
		return context.DeadlineExceeded
	}

	called := false
	dispatchSyntheticMouseMoveOnNodeFunc = func(_ context.Context, nodeID int64, button input.MouseButton, buttons int64) error {
		called = true
		if nodeID != 42 {
			t.Fatalf("nodeID = %d, want 42", nodeID)
		}
		if button != input.Right || buttons != 2 {
			t.Fatalf("button state = (%v, %d), want (%v, 2)", button, buttons, input.Right)
		}
		return nil
	}

	if err := dispatchMouseMoveToNode(context.Background(), 42, 12, 34, input.Right, 2); err != nil {
		t.Fatalf("dispatchMouseMoveToNode returned error: %v", err)
	}
	if !called {
		t.Fatal("expected synthetic node fallback to run")
	}
}

// An unspecified button is a default; an unrecognised NAME is a caller error. Those two
// shared one answer, so a mistyped right-click dispatched a left-click and reported success.
func TestValidateMouseButtonSeparatesUnspecifiedFromUnrecognised(t *testing.T) {
	for _, tc := range []struct {
		in      string
		wantErr bool
		why     string
	}{
		{in: "", why: "unspecified is the default, not an error — refusing it would break every caller that never named one"},
		{in: "   ", why: "whitespace-only is still unspecified"},
		{in: "left"},
		{in: "right"},
		{in: "middle"},
		{in: "RIGHT", why: "case tolerance is worth keeping"},
		{in: " right ", why: "surrounding whitespace is worth keeping"},
		{in: "rihgt", wantErr: true, why: "a misspelling used to dispatch left"},
		{in: "primary", wantErr: true, why: "the DOM vocabulary is refused, not mapped: primary happens to mean left, which is what made this class look harmless"},
		{in: "secondary", wantErr: true, why: "the case primary hides — mapping the DOM names would silently make this left"},
		{in: "0", wantErr: true, why: "a numeric button is not this vocabulary"},
	} {
		err := ValidateMouseButton(tc.in)
		if tc.wantErr && err == nil {
			t.Errorf("ValidateMouseButton(%q) accepted it; %s", tc.in, tc.why)
			continue
		}
		if !tc.wantErr && err != nil {
			t.Errorf("ValidateMouseButton(%q) = %v; %s", tc.in, err, tc.why)
			continue
		}
		if tc.wantErr {
			for _, name := range MouseButtons() {
				if !strings.Contains(err.Error(), name) {
					t.Errorf("ValidateMouseButton(%q) = %v, want it to name the valid button %q", tc.in, err, name)
				}
			}
		}
	}
}

// One vocabulary owner: the normalizer accepts exactly what MouseButtons lists and the
// refusal names exactly that list, so a fourth button added there cannot be accepted in one
// place and refused in another.
func TestTheButtonVocabularyHasOneOwner(t *testing.T) {
	buttons := MouseButtons()
	if len(buttons) < 3 {
		t.Fatalf("MouseButtons() = %v, want at least the three this CLI documents", buttons)
	}
	for _, name := range buttons {
		if err := ValidateMouseButton(name); err != nil {
			t.Errorf("%q is listed as a button but refused: %v", name, err)
		}
		if got := normalizeMouseButton(name); got != name {
			t.Errorf("normalizeMouseButton(%q) = %q; a listed button must survive normalization", name, got)
		}
	}
	if got := normalizeMouseButton(""); got != DefaultMouseButton {
		t.Errorf("normalizeMouseButton(\"\") = %q, want the default %q", got, DefaultMouseButton)
	}
}

// Derived from the callers rather than a hand-listed set: every exported entry point taking
// a button must route it through the normalizer, so none reaches CDP with a raw caller
// string. Adding an entry point that forgets is what this catches.
func TestEveryButtonTakingEntryPointNormalizesIt(t *testing.T) {
	source, err := os.ReadFile("pointer.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "pointer.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}

	checked := 0
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !takesAButton(fn) {
			continue
		}
		if fn.Name.Name == "normalizeMouseButton" || fn.Name.Name == "ValidateMouseButton" {
			continue
		}
		checked++
		if !callsNormalizer(fn) && !passesButtonOn(fn) {
			t.Errorf("%s takes a button and neither normalizes it nor passes it to something that does, so a caller string reaches CDP raw", fn.Name.Name)
		}
	}
	if checked < 4 {
		t.Fatalf("found only %d button-taking functions in pointer.go; this census is not reading the callers", checked)
	}
}

func takesAButton(fn *ast.FuncDecl) bool {
	if fn.Type.Params == nil {
		return false
	}
	for _, param := range fn.Type.Params.List {
		ident, ok := param.Type.(*ast.Ident)
		if !ok || ident.Name != "string" {
			continue
		}
		for _, name := range param.Names {
			if name.Name == "button" {
				return true
			}
		}
	}
	return false
}

func callsNormalizer(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "normalizeMouseButton" {
			found = true
		}
		return true
	})
	return found
}

// A wrapper that hands the button to another button-taking helper is covered by that
// helper's own normalization, so it counts.
func passesButtonOn(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		for _, arg := range call.Args {
			if ident, ok := arg.(*ast.Ident); ok && ident.Name == "button" {
				found = true
			}
		}
		return true
	})
	return found
}

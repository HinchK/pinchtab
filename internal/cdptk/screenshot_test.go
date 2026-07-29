package cdptk_test

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/pinchtab/pinchtab/internal/cdptk"
	"github.com/pinchtab/pinchtab/internal/testbrowser"
)

// An external test package: internal/testbrowser reaches cdptk through
// internal/browsers/chrome, so an in-package test importing it would cycle.

// The page redefines the method both clip builders read. In the main world it
// answers them; an isolated-world handle never sees it.
const hijackFixtureHTML = `<body style="margin:0">
<div id="target" style="position:absolute;left:40px;top:60px;width:120px;height:60px;background:#000"></div>
<script>
(() => {
	Element.prototype.getBoundingClientRect = function () {
		return {x: 999, y: 999, left: 999, top: 999, right: 1099, bottom: 1049, width: 100, height: 50};
	};
})();
</script>
</body>`

func newHijackFixture(t *testing.T) context.Context {
	t.Helper()
	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(testbrowser.Path(t)),
		chromedp.UserDataDir(t.TempDir()),
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

	dataURL := "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(hijackFixtureHTML))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(dataURL),
		chromedp.WaitVisible("#target", chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	return ctx
}

func targetNodeID(t *testing.T, ctx context.Context) int64 {
	t.Helper()
	var nodeID int64
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var doc struct {
			Root struct {
				NodeID int64 `json:"nodeId"`
			} `json:"root"`
		}
		if err := chromedp.FromContext(ctx).Target.Execute(ctx, "DOM.getDocument", map[string]any{"depth": -1}, &doc); err != nil {
			return err
		}
		var found struct {
			NodeID int64 `json:"nodeId"`
		}
		if err := chromedp.FromContext(ctx).Target.Execute(ctx, "DOM.querySelector", map[string]any{
			"nodeId":   doc.Root.NodeID,
			"selector": "#target",
		}, &found); err != nil {
			return err
		}
		var described struct {
			Node struct {
				BackendNodeID int64 `json:"backendNodeId"`
			} `json:"node"`
		}
		if err := chromedp.FromContext(ctx).Target.Execute(ctx, "DOM.describeNode", map[string]any{
			"nodeId": found.NodeID,
		}, &described); err != nil {
			return err
		}
		nodeID = described.Node.BackendNodeID
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	if nodeID == 0 {
		t.Fatal("fixture element #target has no backend node id")
	}
	return nodeID
}

// The cdptk analogue of TestScreenshotClipResistsMainWorldSubstitution: the clip
// origin must come from the element, not from the rectangle page script forges.
func TestClipForNodeResistsMainWorldSubstitution(t *testing.T) {
	ctx := newHijackFixture(t)
	nodeID := targetNodeID(t, ctx)

	clip, err := cdptk.ClipForNode(ctx, nodeID, true)
	if err != nil {
		t.Fatalf("ClipForNode: %v", err)
	}

	// The forged rect is the one value the page can produce; anything else means
	// the read happened somewhere the page could not reach.
	if clip.X == 999 || clip.Y == 999 || (clip.Width == 100 && clip.Height == 50) {
		t.Errorf("clip %+v matches the rectangle page script forged; the origin was read in the main world", clip)
	}
	if clip.Width != 120 || clip.Height != 60 {
		t.Errorf("clip %+v does not match the element's real 120x60 box", clip)
	}
}

// The same boundary on the annotation rect: its frame walk reads
// getBoundingClientRect too, and its output places the overlay boxes a vision
// model is told to read.
func TestAnnotationRectForNodeResistsMainWorldSubstitution(t *testing.T) {
	ctx := newHijackFixture(t)
	nodeID := targetNodeID(t, ctx)

	rect, err := cdptk.AnnotationRectForNode(ctx, nodeID)
	if err != nil {
		t.Fatalf("AnnotationRectForNode: %v", err)
	}
	if rect == nil {
		t.Fatal("AnnotationRectForNode returned no rect for a top-frame element")
	}

	if rect.X == 999 || rect.Y == 999 || (rect.W == 100 && rect.H == 50) {
		t.Errorf("rect %+v matches the rectangle page script forged; it was read in the main world", rect)
	}
	if rect.W != 120 || rect.H != 60 {
		t.Errorf("rect %+v does not match the element's real 120x60 box", rect)
	}
	if rect.X != 40 || rect.Y != 60 {
		t.Errorf("rect %+v is not the element's viewport position (40,60)", rect)
	}
}

// The iframe sits at (400,300) and #inner at (10,20) inside it, so a clip that
// carries the frame offset lands at (410,320) and a frame-local one at (10,20).
const framedFixtureHTML = `<body style="margin:0">
<iframe id="f" style="position:absolute;left:400px;top:300px;border:0" width="300" height="200"
	srcdoc="<body style='margin:0'><div id='inner' style='position:absolute;left:10px;top:20px;width:80px;height:40px;background:#00c'></div></body>"></iframe>
</body>`

func newFramedFixture(t *testing.T) context.Context {
	t.Helper()
	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(testbrowser.Path(t)),
		chromedp.UserDataDir(t.TempDir()),
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

	dataURL := "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(framedFixtureHTML))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(dataURL),
		chromedp.WaitVisible("#f", chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	return ctx
}

func innerFrameNodeID(t *testing.T, ctx context.Context) int64 {
	t.Helper()
	var nodeID int64
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		exec := chromedp.FromContext(ctx).Target
		var doc struct {
			Root struct {
				NodeID int64 `json:"nodeId"`
			} `json:"root"`
		}
		if err := exec.Execute(ctx, "DOM.getDocument", map[string]any{"depth": -1, "pierce": true}, &doc); err != nil {
			return err
		}
		var frame struct {
			NodeID int64 `json:"nodeId"`
		}
		if err := exec.Execute(ctx, "DOM.querySelector", map[string]any{
			"nodeId": doc.Root.NodeID, "selector": "#f",
		}, &frame); err != nil {
			return err
		}
		var described struct {
			Node struct {
				ContentDocument struct {
					NodeID int64 `json:"nodeId"`
				} `json:"contentDocument"`
			} `json:"node"`
		}
		if err := exec.Execute(ctx, "DOM.describeNode", map[string]any{
			"nodeId": frame.NodeID, "pierce": true, "depth": -1,
		}, &described); err != nil {
			return err
		}
		var inner struct {
			NodeID int64 `json:"nodeId"`
		}
		if err := exec.Execute(ctx, "DOM.querySelector", map[string]any{
			"nodeId": described.Node.ContentDocument.NodeID, "selector": "#inner",
		}, &inner); err != nil {
			return err
		}
		var innerDesc struct {
			Node struct {
				BackendNodeID int64 `json:"backendNodeId"`
			} `json:"node"`
		}
		if err := exec.Execute(ctx, "DOM.describeNode", map[string]any{"nodeId": inner.NodeID}, &innerDesc); err != nil {
			return err
		}
		nodeID = innerDesc.Node.BackendNodeID
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	if nodeID == 0 {
		t.Fatal("fixture element #inner has no backend node id")
	}
	return nodeID
}

// An isolated-world handle runs in the TOP frame's world, where a bare `window`
// has an empty frameElement chain — so the frame walk must start from the node's
// own view or every frame offset is silently dropped.
func TestClipForNodeAppliesFrameOffset(t *testing.T) {
	ctx := newFramedFixture(t)

	clip, err := cdptk.ClipForNode(ctx, innerFrameNodeID(t, ctx), true)
	if err != nil {
		t.Fatalf("clip for in-frame element: %v", err)
	}

	const wantX, wantY, wantW, wantH = 410, 320, 80, 40
	if clip.X != wantX || clip.Y != wantY {
		t.Errorf("in-frame clip origin = (%.0f,%.0f), want (%d,%d) — frame offset not applied (frame-local origin is (10,20))",
			clip.X, clip.Y, wantX, wantY)
	}
	if clip.Width != wantW || clip.Height != wantH {
		t.Errorf("in-frame clip size = %.0fx%.0f, want %dx%d", clip.Width, clip.Height, wantW, wantH)
	}
}

// The same isolated-world frame-walk trap as the clip builder, on the path that
// actually has production callers: the annotate handler places the overlay boxes
// a vision model is told to read, so a frame-local rect misplaces every one.
func TestAnnotationRectForNodeAppliesFrameOffset(t *testing.T) {
	ctx := newFramedFixture(t)

	rect, err := cdptk.AnnotationRectForNode(ctx, innerFrameNodeID(t, ctx))
	if err != nil {
		t.Fatalf("AnnotationRectForNode for in-frame element: %v", err)
	}
	if rect == nil {
		t.Fatal("AnnotationRectForNode returned no rect for an in-frame element")
	}

	const wantX, wantY, wantW, wantH = 410, 320, 80, 40
	if rect.X != wantX || rect.Y != wantY {
		t.Errorf("in-frame rect origin = (%.0f,%.0f), want (%d,%d) — frame offset not applied (frame-local origin is (10,20))",
			rect.X, rect.Y, wantX, wantY)
	}
	if rect.W != wantW || rect.H != wantH {
		t.Errorf("in-frame rect size = %.0fx%.0f, want %dx%d", rect.W, rect.H, wantW, wantH)
	}
}

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

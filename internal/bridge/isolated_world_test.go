package bridge

import (
	"context"
	"encoding/base64"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/pinchtab/pinchtab/internal/selector"
	"github.com/pinchtab/pinchtab/internal/testbrowser"
)

// The page attempts the substitution the isolated world exists to prevent. Every
// redefinition is real page script, not a mock: querySelectorAll and matches are
// what the scoped selector resolver calls, and getBoundingClientRect is what the
// clip builder calls. In the main world all three answer the attacker.
const hijackFixtureHTML = `<body>
<div id="dlg" role="dialog" aria-modal="true" style="position:fixed;inset:20px;background:#fff">
<button id="real">Confirm</button>
<button id="decoy">Decoy</button>
</div>
<script>
(() => {
	const decoy = document.getElementById('decoy');
	const realQSA = Element.prototype.querySelectorAll;
	Element.prototype.querySelectorAll = function () { return [decoy]; };
	Element.prototype.matches = function () { return true; };
	Element.prototype.getBoundingClientRect = function () {
		return {x: 999, y: 999, left: 999, top: 999, right: 1099, bottom: 1049, width: 100, height: 50};
	};
	window.__realQSA = realQSA;
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
		chromedp.WaitVisible("#real", chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	return ctx
}

// nodeDescription names what a backend node id actually is, so a failure reads
// "button#decoy" rather than two opaque numbers.
func nodeDescription(t *testing.T, ctx context.Context, backendNodeID int64) string {
	t.Helper()
	objectID, err := IsolatedNodeObjectID(ctx, backendNodeID)
	if err != nil {
		t.Fatalf("describe node %d: %v", backendNodeID, err)
	}
	var call struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return chromedp.FromContext(ctx).Target.Execute(ctx, "Runtime.callFunctionOn", map[string]any{
			"functionDeclaration": `function(){ return this.tagName.toLowerCase() + (this.id ? "#" + this.id : ""); }`,
			"objectId":            objectID,
			"returnByValue":       true,
		}, &call)
	})); err != nil {
		t.Fatalf("describe node %d: %v", backendNodeID, err)
	}
	return call.Result.Value
}

// The page redefines the very methods the dialog-scoped resolver calls. A
// main-world resolution hands back whatever they return; an isolated-world one
// never sees them.
func TestDialogScopedSelectorResistsMainWorldSubstitution(t *testing.T) {
	ctx := newHijackFixture(t)

	dialogNodeID, open, err := TopmostModalNodeID(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if !open {
		t.Fatal("fixture dialog was not detected")
	}

	got, err := ResolveUnifiedSelectorWithinNode(ctx, selector.Parse("css:#real"), nil, dialogNodeID)
	if err != nil {
		t.Fatalf("scoped resolve: %v", err)
	}
	if desc := nodeDescription(t, ctx, got); desc != "button#real" {
		t.Errorf("dialog-scoped css:#real resolved to %s; page script steered the resolution", desc)
	}
}

// getBoundingClientRect is redefined to report a rectangle far from the element.
// If the clip is built in the main world the capture is aimed at the attacker's
// coordinates instead of the element's.
func TestScreenshotClipResistsMainWorldSubstitution(t *testing.T) {
	ctx := newHijackFixture(t)

	nodeID, err := ResolveCSSToNodeID(ctx, "#real")
	if err != nil {
		t.Fatal(err)
	}
	clip, err := ScreenshotClipForNode(ctx, nodeID)
	if err != nil {
		t.Fatalf("clip: %v", err)
	}

	// The forged rect is the one value the page can produce; anything else means
	// the read happened somewhere the page could not reach.
	if clip.X == 999 || clip.Y == 999 || (clip.Width == 100 && clip.Height == 50) {
		t.Errorf("clip %+v matches the rectangle page script forged; the origin was read in the main world", clip)
	}
	if clip.Width <= 0 || clip.Height <= 0 {
		t.Errorf("clip %+v has no area", clip)
	}
}

// The boundary must fail closed. If no isolated context can be obtained the
// resolution has to error — falling back to a main-world handle would leave the
// hole open with every test above still green.
func TestIsolatedNodeObjectIDFailsClosedWithoutContext(t *testing.T) {
	ctx := newHijackFixture(t)

	nodeID, err := ResolveCSSToNodeID(ctx, "#real")
	if err != nil {
		t.Fatal(err)
	}

	// A cancelled context cannot create an isolated world, which is the closest
	// reachable stand-in for a frame that cannot supply one.
	dead, cancel := context.WithCancel(ctx)
	cancel()

	if objectID, err := IsolatedNodeObjectID(dead, nodeID); err == nil {
		t.Fatalf("resolution succeeded with object %q when no isolated context was obtainable; it must not fall back to the main world", objectID)
	}
}

// mainWorldResolvers is the boundary as it actually stands, as data. These files
// still resolve a bare backend node id, so their Runtime.callFunctionOn work runs
// in the main world. They are the OPERATION paths — what is done to a node once
// chosen — which this change deliberately does not cover; extending the boundary
// across the action layer is a larger question with its own risk profile.
//
// The resolution paths, which decide WHICH element is acted on, are not here:
// that is the whole claim, and this table is what keeps the claim honest. A new
// unprotected site appearing, or one of these being fixed without updating the
// list, both fail here.
// Keys are module-relative paths: pinchtab spreads CDP work over internal/bridge,
// internal/bridge/cdpops and internal/cdptk, and a census of one directory would
// report a clean boundary while three packages went unchecked.
var mainWorldResolvers = map[string]string{
	"internal/bridge/action_form.go":        "form fill operates on an already-chosen field",
	"internal/bridge/semantic_metadata.go":  "metadata enrichment reads an already-chosen node",
	"internal/bridge/tab_auto_switch.go":    "tab switching acts on an already-chosen target",
	"internal/bridge/cdpops/element_ops.go": "element ops act on an already-chosen node",
	"internal/bridge/cdpops/frame_dom.go":   "callFunctionOn helper for an already-chosen node",
	"internal/bridge/cdpops/pointer.go":     "pointer actions act on an already-chosen node",
	"internal/bridge/cdpops/validation.go":  "post-action validation reads an already-chosen node",
	"internal/bridge/cdpops/geometry.go":    "click-point geometry for an already-chosen node",
}

func TestIsolatedWorldBoundaryCensus(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	found := map[string]bool{}
	checked := 0
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		name := filepath.ToSlash(rel)
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, block := range strings.Split(string(raw), `"DOM.resolveNode"`)[1:] {
			checked++
			head := block
			if len(head) > 200 {
				head = head[:200]
			}
			if strings.Contains(head, "executionContextId") {
				continue
			}
			found[name] = true
			if _, known := mainWorldResolvers[name]; !known {
				t.Errorf("%s resolves a node without an isolated executionContextId and is not a recorded operation path:\n%s", name, head)
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	if checked == 0 {
		t.Fatal("no DOM.resolveNode call found in the module — this census is checking nothing")
	}
	for name, why := range mainWorldResolvers {
		if !found[name] {
			t.Errorf("%s no longer resolves in the main world (%s); remove it from mainWorldResolvers so the recorded boundary stays true", name, why)
		}
	}
}

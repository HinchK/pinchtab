package bridge

import (
	"context"
	"encoding/base64"
	"os"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
	"github.com/pinchtab/pinchtab/internal/testbrowser"
)

// Two independent pairs in one page: #mouseSrc/#mouseDst answer plain mousedown/mouseup,
// #h5Src/#h5Dst are real HTML5 drag-and-drop. A page-level record is what has to be
// asserted — the action's own dragged:true was already there while nothing happened.
const dragFixtureHTML = `<style>div{width:120px;height:80px;display:inline-block;border:1px solid #000}</style>
<div id="h5Src" draggable="true">h5 src</div><div id="h5Dst">h5 dst</div><br>
<div id="mouseSrc">mouse src</div><div id="mouseDst">mouse dst</div>
<script>
window.h5 = 'no';
window.mouse = 'no';
const h5Src = document.getElementById('h5Src'), h5Dst = document.getElementById('h5Dst');
h5Src.addEventListener('dragstart', e => { window.h5 = 'started'; e.dataTransfer.setData('text/plain', 'payload'); });
h5Dst.addEventListener('dragover', e => e.preventDefault());
h5Dst.addEventListener('drop', e => { e.preventDefault(); window.h5 = 'DROPPED'; });
const mouseSrc = document.getElementById('mouseSrc'), mouseDst = document.getElementById('mouseDst');
let pressed = false;
mouseSrc.addEventListener('mousedown', () => { pressed = true; });
mouseDst.addEventListener('mouseup', () => { if (pressed) window.mouse = 'DROPPED'; });
</script>`

type dragFixture struct {
	ctx context.Context
	b   *Bridge
}

func newDragFixture(t *testing.T) *dragFixture {
	t.Helper()
	chromePath := testbrowser.Path(t)

	profile, err := os.MkdirTemp("", "pinchtab-drag-")
	if err != nil {
		t.Fatal(err)
	}
	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.UserDataDir(profile),
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
	)...)
	ctx, cancelBrowser := chromedp.NewContext(alloc)
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	t.Cleanup(func() {
		cancelTimeout()
		cancelBrowser()
		cancelAlloc()
		_ = os.RemoveAll(profile)
	})

	return &dragFixture{ctx: ctx, b: &Bridge{}}
}

// Each trial reloads, so one trial cannot inherit another's drop.
func (f *dragFixture) reload(t *testing.T) {
	t.Helper()
	url := "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(dragFixtureHTML))
	if err := chromedp.Run(f.ctx, chromedp.Navigate(url)); err != nil {
		t.Fatal(err)
	}
}

func (f *dragFixture) state(t *testing.T) (html5 string, mouse string) {
	t.Helper()
	if err := chromedp.Run(f.ctx,
		chromedp.Evaluate(`window.h5`, &html5),
		chromedp.Evaluate(`window.mouse`, &mouse),
	); err != nil {
		t.Fatal(err)
	}
	return html5, mouse
}

func (f *dragFixture) nodeID(t *testing.T, selector string) int64 {
	t.Helper()
	var nodes []*cdp.Node
	if err := chromedp.Run(f.ctx, chromedp.Nodes(selector, &nodes, chromedp.ByQuery)); err != nil {
		t.Fatalf("%s: %v", selector, err)
	}
	if len(nodes) == 0 {
		t.Fatalf("%s matched nothing", selector)
	}
	return int64(nodes[0].BackendNodeID)
}

func (f *dragFixture) point(t *testing.T, selector string) (float64, float64) {
	t.Helper()
	x, y, err := PointerPointForNode(f.ctx, f.nodeID(t, selector), true)
	if err != nil {
		t.Fatalf("%s: %v", selector, err)
	}
	return x, y
}

// The defect: `drag <from> <to>` reported three OKs and did nothing on an HTML5 draggable,
// while the offset form worked. Both forms now express the same drag, so both complete it.
func TestDragCompletesAnHTML5DropInBothForms(t *testing.T) {
	f := newDragFixture(t)

	for _, tc := range []struct {
		name string
		body func(t *testing.T) ActionRequest
	}{
		{
			name: "target form",
			body: func(t *testing.T) ActionRequest {
				return ActionRequest{Kind: ActionDrag, Selector: "#h5Src", ToSelector: "#h5Dst"}
			},
		},
		{
			// What the handler hands the bridge once it has resolved a ref or a semantic
			// selector: the destination as a node id.
			name: "target form by node id",
			body: func(t *testing.T) ActionRequest {
				return ActionRequest{Kind: ActionDrag, NodeID: f.nodeID(t, "#h5Src"), ToNodeID: f.nodeID(t, "#h5Dst")}
			},
		},
		{
			name: "target form by coordinates",
			body: func(t *testing.T) ActionRequest {
				x, y := f.point(t, "#h5Dst")
				return ActionRequest{Kind: ActionDrag, Selector: "#h5Src", ToX: x, ToY: y, HasToXY: true}
			},
		},
		{
			name: "offset form",
			body: func(t *testing.T) ActionRequest {
				srcX, srcY := f.point(t, "#h5Src")
				dstX, dstY := f.point(t, "#h5Dst")
				return ActionRequest{Kind: ActionDrag, Selector: "#h5Src", DragX: int(dstX - srcX), DragY: int(dstY - srcY)}
			},
		},
	} {
		f.reload(t)
		req := tc.body(t)

		result, err := f.b.actionDrag(f.ctx, req)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if result["dragged"] != true {
			t.Errorf("%s: result = %v, want dragged:true", tc.name, result)
		}

		html5, _ := f.state(t)
		if html5 != "DROPPED" {
			t.Errorf("%s: page recorded h5=%q, want DROPPED; %q means dragstart never fired at all", tc.name, html5, "no")
		}
	}
}

// The mouse-event pair works today and must keep working: the fix cannot be a trade of one
// drag style for the other.
func TestDragStillCompletesAMouseDrivenDrop(t *testing.T) {
	f := newDragFixture(t)
	f.reload(t)

	if _, err := f.b.actionDrag(f.ctx, ActionRequest{Kind: ActionDrag, Selector: "#mouseSrc", ToSelector: "#mouseDst"}); err != nil {
		t.Fatal(err)
	}

	_, mouse := f.state(t)
	if mouse != "DROPPED" {
		t.Errorf("page recorded mouse=%q, want DROPPED", mouse)
	}
}

// The two forms are documented as forms of one command, so the same source and destination
// must produce the same page state whichever one expresses it.
func TestBothDragFormsProduceTheSameOutcome(t *testing.T) {
	f := newDragFixture(t)

	f.reload(t)
	if _, err := f.b.actionDrag(f.ctx, ActionRequest{Kind: ActionDrag, Selector: "#h5Src", ToSelector: "#h5Dst"}); err != nil {
		t.Fatal(err)
	}
	targetH5, targetMouse := f.state(t)

	f.reload(t)
	srcX, srcY := f.point(t, "#h5Src")
	dstX, dstY := f.point(t, "#h5Dst")
	if _, err := f.b.actionDrag(f.ctx, ActionRequest{Kind: ActionDrag, Selector: "#h5Src", DragX: int(dstX - srcX), DragY: int(dstY - srcY)}); err != nil {
		t.Fatal(err)
	}
	offsetH5, offsetMouse := f.state(t)

	if targetH5 != offsetH5 || targetMouse != offsetMouse {
		t.Errorf("target form left (h5=%q mouse=%q), offset form left (h5=%q mouse=%q); the help presents them as forms of one command", targetH5, targetMouse, offsetH5, offsetMouse)
	}
}

// A drop onto an overlapping or same-position target is a legitimate request. The offset
// form has to refuse 0,0 — it means "no movement asked for" — but the target form must not
// inherit a refusal written for a different question.
func TestATargetDragOntoTheSourceIsNotRefusedAsAZeroOffset(t *testing.T) {
	f := newDragFixture(t)
	f.reload(t)

	if _, err := f.b.actionDrag(f.ctx, ActionRequest{Kind: ActionDrag, Selector: "#h5Src", ToSelector: "#h5Src"}); err != nil {
		t.Fatalf("a drag onto its own source was refused: %v", err)
	}

	if _, err := f.b.actionDrag(f.ctx, ActionRequest{Kind: ActionDrag, Selector: "#h5Src"}); err == nil {
		t.Error("the offset form accepted a drag with no offset and no destination, so a caller who forgot both reads it as success")
	}
}

// The individual pointer primitives are fine for what they are, and the fix must not have
// changed what they do for everyone else: four requests still move the pointer in one jump,
// which is exactly why `drag` can no longer be assembled out of them.
func TestThePointerPrimitivesStillDispatchOneJumpEach(t *testing.T) {
	f := newDragFixture(t)

	f.reload(t)
	f.handAssembledDrag(t, "#mouseSrc", "#mouseDst")
	if _, mouse := f.state(t); mouse != "DROPPED" {
		t.Errorf("the primitives stopped completing a mouse-driven drop: mouse=%q", mouse)
	}

	f.reload(t)
	f.handAssembledDrag(t, "#h5Src", "#h5Dst")
	if html5, _ := f.state(t); html5 != "no" {
		t.Errorf("hand-assembled pointer steps recorded h5=%q; if they now complete an HTML5 drop, this test records an obsolete limitation and `drag` no longer needs to own the sequence", html5)
	}
}

// handAssembledDrag is the sequence the CLI used to post as four independent requests: one
// move to the source, a press, ONE move to the destination, a release.
func (f *dragFixture) handAssembledDrag(t *testing.T, from, to string) {
	t.Helper()

	srcX, srcY := f.point(t, from)
	dstX, dstY := f.point(t, to)
	for _, step := range []struct {
		kind   string
		action ActionFunc
		req    ActionRequest
	}{
		{kind: ActionMouseMove, action: f.b.actionMouseMove, req: ActionRequest{X: srcX, Y: srcY, HasXY: true}},
		{kind: ActionMouseDown, action: f.b.actionMouseDown, req: ActionRequest{X: srcX, Y: srcY, HasXY: true}},
		{kind: ActionMouseMove, action: f.b.actionMouseMove, req: ActionRequest{X: dstX, Y: dstY, HasXY: true}},
		{kind: ActionMouseUp, action: f.b.actionMouseUp, req: ActionRequest{X: dstX, Y: dstY, HasXY: true}},
	} {
		if _, err := step.action(f.ctx, step.req); err != nil {
			t.Fatalf("%s: %v", step.kind, err)
		}
	}
}

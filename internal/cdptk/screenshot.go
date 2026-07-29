package cdptk

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chromedp/chromedp"
)

func ClipForNode(ctx context.Context, nodeID int64, css1x bool) (*ScreenshotClip, error) {
	// Bring target element into view before computing clip coordinates.
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return chromedp.FromContext(ctx).Target.Execute(ctx, "DOM.scrollIntoViewIfNeeded", map[string]any{
			"backendNodeId": nodeID,
		}, nil)
	})); err != nil {
		return nil, fmt.Errorf("scroll into view: %w", err)
	}

	// Isolated world: boxFn reads getBoundingClientRect, so a main-world handle
	// would let the page steer the clip origin by redefining it.
	objectID, err := IsolatedNodeObjectID(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("resolve node: %w", err)
	}

	// Translate the element box into top-level page coordinates. captureScreenshot
	// clip coordinates are page-relative, so viewport-relative rects need scroll
	// offsets from the current document and each ancestor frame.
	// The frame walk starts from the NODE's own view for the same reason as the
	// bridge clip builder: the isolated handle runs in the top frame's world, so a
	// bare `window` there loses every frame offset.
	const boxFn = `function() {
		const view = (this.ownerDocument && this.ownerDocument.defaultView) || window;
		const rect = this.getBoundingClientRect();
		let x = rect.left + (view.scrollX || view.pageXOffset || 0);
		let y = rect.top + (view.scrollY || view.pageYOffset || 0);
		try {
			let current = view;
			while (current && current.parent && current !== current.parent) {
				const frameEl = current.frameElement;
				if (!frameEl) {
					break;
				}
				const parent = current.parent;
				const frameRect = frameEl.getBoundingClientRect();
				x += frameRect.left + (parent.scrollX || parent.pageXOffset || 0);
				y += frameRect.top + (parent.scrollY || parent.pageYOffset || 0);
				current = parent;
			}
		} catch (e) {
			// Cross-origin ancestors can block frame traversal. Keep the deepest
			// reachable page coordinates in that case.
		}
		return {
			x,
			y,
			width: rect.width,
			height: rect.height,
			dpr: view.devicePixelRatio || 1
		};
	}`

	var callResult json.RawMessage
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return chromedp.FromContext(ctx).Target.Execute(ctx, "Runtime.callFunctionOn", map[string]any{
			"functionDeclaration": boxFn,
			"objectId":            objectID,
			"returnByValue":       true,
		}, &callResult)
	})); err != nil {
		return nil, fmt.Errorf("read element box: %w", err)
	}

	var boxCall struct {
		Result struct {
			Value struct {
				X      float64 `json:"x"`
				Y      float64 `json:"y"`
				Width  float64 `json:"width"`
				Height float64 `json:"height"`
				DPR    float64 `json:"dpr"`
			} `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(callResult, &boxCall); err != nil {
		return nil, fmt.Errorf("parse element box: %w", err)
	}

	box := boxCall.Result.Value
	if box.Width <= 0 || box.Height <= 0 {
		return nil, fmt.Errorf("element box is empty (width=%.2f height=%.2f)", box.Width, box.Height)
	}
	scale := 1.0
	if css1x {
		if box.DPR <= 0 {
			box.DPR = 1
		}
		scale = 1 / box.DPR
	}

	return &ScreenshotClip{
		X:      box.X,
		Y:      box.Y,
		Width:  box.Width,
		Height: box.Height,
		Scale:  scale,
	}, nil
}

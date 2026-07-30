package cdpops

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chromedp/chromedp"
)

// GetElementCenterJS resolves the DOM node and evaluates getBoundingClientRect().
func GetElementCenterJS(ctx context.Context, backendNodeID int64) (float64, float64, error) {
	var resolveResult json.RawMessage
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return chromedp.FromContext(ctx).Target.Execute(ctx, "DOM.resolveNode", map[string]any{
			"backendNodeId": backendNodeID,
		}, &resolveResult)
	})); err != nil {
		return 0, 0, fmt.Errorf("DOM.resolveNode: %w", err)
	}

	var resolved struct {
		Object struct {
			ObjectID string `json:"objectId"`
		} `json:"object"`
	}
	if err := json.Unmarshal(resolveResult, &resolved); err != nil {
		return 0, 0, err
	}
	if resolved.Object.ObjectID == "" {
		return 0, 0, fmt.Errorf("element not found in DOM (backendNodeId=%d)", backendNodeID)
	}

	const rectFn = `function() {
		var r = this.getBoundingClientRect();
		return { x: r.left + r.width / 2, y: r.top + r.height / 2 };
	}`

	var callResult json.RawMessage
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return chromedp.FromContext(ctx).Target.Execute(ctx, "Runtime.callFunctionOn", map[string]any{
			"functionDeclaration": rectFn,
			"objectId":            resolved.Object.ObjectID,
			"returnByValue":       true,
		}, &callResult)
	})); err != nil {
		return 0, 0, fmt.Errorf("getBoundingClientRect: %w", err)
	}

	var callRes struct {
		Result struct {
			Value struct {
				X float64 `json:"x"`
				Y float64 `json:"y"`
			} `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(callResult, &callRes); err != nil {
		return 0, 0, err
	}

	return callRes.Result.Value.X, callRes.Result.Value.Y, nil
}

func ScrollIntoViewIfNeeded(ctx context.Context, nodeID int64) error {
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return chromedp.FromContext(ctx).Target.Execute(ctx, "DOM.scrollIntoViewIfNeeded", map[string]any{"backendNodeId": nodeID}, nil)
	})); err != nil {
		return fmt.Errorf("scrollIntoViewIfNeeded: %w", err)
	}
	return nil
}

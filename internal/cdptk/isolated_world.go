package cdptk

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chromedp/chromedp"
)

// isolatedWorldName labels the world reused for node resolution.
// Page.createIsolatedWorld keys on the name, so repeat calls return the same
// context rather than minting one per resolution.
const isolatedWorldName = "pinchtab-node-scope"

// IsolatedNodeObjectID converts a backend node id to a JS object handle in the
// top frame's isolated world.
//
// DOM.resolveNode without an executionContextId hands back a main-world object,
// and every Runtime.callFunctionOn against it then runs where page script can
// redefine the DOM methods it uses — so geometry read through such a handle is
// whatever the page says it is, not where the element actually sits.
//
// The isolated world is per-frame, but a handle from any frame's world reaches a
// node in another same-process frame, so the top frame's world serves all of
// them: a bare backend node id does not carry its frame, and DOM.describeNode
// reports frameId only for frame owner elements.
//
// It never returns a usable zero. A caller that cannot obtain an isolated
// context gets an error rather than a main-world handle, so the boundary fails
// closed.
func IsolatedNodeObjectID(ctx context.Context, backendNodeID int64) (string, error) {
	execID, err := topFrameIsolatedContextID(ctx)
	if err != nil {
		return "", err
	}

	var raw json.RawMessage
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return chromedp.FromContext(ctx).Target.Execute(ctx, "DOM.resolveNode", map[string]any{
			"backendNodeId":      backendNodeID,
			"executionContextId": execID,
		}, &raw)
	})); err != nil {
		return "", err
	}

	var parsed struct {
		Object struct {
			ObjectID string `json:"objectId"`
		} `json:"object"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if parsed.Object.ObjectID == "" {
		return "", fmt.Errorf("backend node %d is no longer attached", backendNodeID)
	}
	return parsed.Object.ObjectID, nil
}

func topFrameIsolatedContextID(ctx context.Context) (int64, error) {
	// GetFrameTree issues a CDP call, so it needs an executor context; calling it
	// with the caller's bare ctx fails with "invalid context".
	var frameID string
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		tree, err := GetFrameTree(ctx)
		if err != nil {
			return err
		}
		if tree != nil && tree.Frame != nil {
			frameID = tree.Frame.ID.String()
		}
		return nil
	})); err != nil {
		return 0, fmt.Errorf("resolve top frame: %w", err)
	}
	if frameID == "" {
		return 0, fmt.Errorf("resolve top frame: frame id is empty")
	}

	var raw json.RawMessage
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return chromedp.FromContext(ctx).Target.Execute(ctx, "Page.createIsolatedWorld", map[string]any{
			"frameId":   frameID,
			"worldName": isolatedWorldName,
		}, &raw)
	})); err != nil {
		return 0, fmt.Errorf("create isolated world: %w", err)
	}

	var resp struct {
		ExecutionContextID int64 `json:"executionContextId"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 0, err
	}
	if resp.ExecutionContextID == 0 {
		return 0, fmt.Errorf("top frame has no isolated execution context")
	}
	return resp.ExecutionContextID, nil
}

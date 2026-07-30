package mcp

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// humanizeAction is the set of pointer actions that accept a per-request humanize
// override, mirroring the CLI's addPointerActionFlags set. Only these tools
// declare humanize in their schema; focus is not a pointer action, and only click
// also has mode, so only click can hit the mutual-exclusion rule.
var humanizeAction = map[string]bool{"click": true, "hover": true}

// xyAction is the set of kinds that can be targeted by coordinate, mirroring the
// tools that declare x/y. The bridge honours x/y only for these kinds — the text
// and form actions target by nodeId or selector — so reading it for every kind
// forwarded an inert, undiscoverable and unvalidated pair, hasXY included.
var xyAction = map[string]bool{"click": true, "hover": true, "scroll": true}

func handleAction(c *Client, kind string) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		payload := map[string]any{"kind": kind}

		if tabID := optString(r, "tabId"); tabID != "" {
			payload["tabId"] = tabID
		}

		x, y, hasXY := 0.0, 0.0, false
		if xyAction[kind] {
			x, y, hasXY = resolveXY(r)
		}
		if hasXY {
			payload["x"] = x
			payload["y"] = y
			payload["hasXY"] = true
		}

		hasNodeID := false
		if nodeID, ok := optInt(r, "nodeId"); ok && nodeID > 0 {
			hasNodeID = true
			payload["nodeId"] = nodeID
		}

		resolveSelector := func(required bool) (bool, error) {
			sel := actionSelectorArg(r)
			if sel != "" {
				payload["selector"] = sel
				return true, nil
			}
			if required {
				return false, fmt.Errorf("required parameter 'selector' is missing")
			}
			return false, nil
		}

		switch kind {
		case "click", "hover", "focus":
			requiresSelector := !hasNodeID
			if kind == "click" || kind == "hover" {
				requiresSelector = requiresSelector && !hasXY
			}
			if _, err := resolveSelector(requiresSelector); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			// Forwarded only when the caller actually sent it, so an omitted
			// humanize inherits the instance default instead of overriding it for
			// every request. An explicit false is a real opt-out and travels: the
			// wire field is a per-request override, not a flag.
			humanize, humanizeSet := false, false
			if humanizeAction[kind] {
				humanize, humanizeSet = optBool(r, "humanize")
				if humanizeSet {
					payload["humanize"] = humanize
				}
			}
			if kind == "click" {
				if waitNav, ok := optBool(r, "waitNav"); ok && waitNav {
					payload["waitNav"] = true
				}
				if mode := strings.ToLower(strings.TrimSpace(optTrimmedString(r, "mode"))); mode != "" {
					if mode != "dom" && mode != "dispatch" {
						return mcp.NewToolResultError("mode must be 'dom' or 'dispatch'"), nil
					}
					if humanizeSet && humanize {
						return mcp.NewToolResultError("mode and humanize are mutually exclusive"), nil
					}
					payload["mode"] = mode
				}
				dialogAction := strings.ToLower(firstNonEmptyString(r, "dialogAction", "onDialog"))
				if dialogAction != "" {
					if dialogAction != "accept" && dialogAction != "dismiss" {
						return mcp.NewToolResultError("dialogAction must be 'accept' or 'dismiss'"), nil
					}
					payload["dialogAction"] = dialogAction
					if dialogText := firstNonEmptyString(r, "dialogText", "promptText"); dialogText != "" {
						payload["dialogText"] = dialogText
					}
				}
			}

		case "type":
			if _, err := resolveSelector(!hasNodeID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			text := firstNonEmptyString(r, "text", "value")
			if text == "" {
				return mcp.NewToolResultError("required parameter 'text' is missing"), nil
			}
			payload["text"] = text

		case "press":
			key, err := r.RequireString("key")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			payload["key"] = key

		case "select":
			if _, err := resolveSelector(!hasNodeID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			value := firstNonEmptyString(r, "value", "option")
			if value == "" {
				return mcp.NewToolResultError("required parameter 'value' is missing"), nil
			}
			payload["value"] = value

		case "scroll":
			hasSelector, err := resolveSelector(false)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			pixels, hasPixels := optInt(r, "pixels")
			deltaX, hasDeltaX := optInt(r, "deltaX")
			deltaY, hasDeltaY := optInt(r, "deltaY")

			direction := strings.ToLower(optTrimmedString(r, "direction"))
			steps, hasSteps := optInt(r, "steps")
			if !hasSteps || steps < 1 {
				steps = 1
			}

			if direction != "" && !hasDeltaY {
				magnitude := 120
				if hasPixels && pixels != 0 {
					magnitude = pixels
					if magnitude < 0 {
						magnitude = -magnitude
					}
				}
				magnitude *= steps
				switch direction {
				case "down":
					deltaY = magnitude
				case "up":
					deltaY = -magnitude
				default:
					return mcp.NewToolResultError("direction must be 'up' or 'down'"), nil
				}
				hasDeltaY = true
			}

			useWheel := hasXY || hasDeltaX || hasDeltaY || (hasSelector && hasPixels)
			if useWheel {
				payload["kind"] = "mouse-wheel"
				if hasDeltaX {
					payload["deltaX"] = deltaX
				}
				if hasDeltaY {
					payload["deltaY"] = deltaY
				} else if hasPixels {
					payload["deltaY"] = pixels
				}
			} else if hasPixels {
				payload["scrollY"] = pixels
			}

		case "scrollintoview":
			if _, err := resolveSelector(!hasNodeID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

		case "fill":
			if _, err := resolveSelector(!hasNodeID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			// Presence, not emptiness: an empty value is how a caller clears a field,
			// so refusing it made the one documented clear idiom inexpressible here
			// while the raw API accepted it — and the old message claimed a parameter
			// the caller had supplied was missing.
			value, supplied := firstSuppliedString(r, "value", "text")
			if !supplied {
				return mcp.NewToolResultError("fill needs a 'value' argument; send \"\" to clear the field"), nil
			}
			// "text" is the field actionFill reads. Posting the same string under "value"
			// reached a real ActionRequest field that fill ignores, so the write was empty
			// and the tool still answered filled:true.
			payload["text"] = value
		}

		body, code, err := c.Post(ctx, routedPathWithBody(r, "/action", payload), payload)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if code >= 400 {
			return resultFromBytes(body, code)
		}

		snapSupportedKinds := map[string]bool{"click": true, "fill": true, "select": true}
		if snapSupportedKinds[kind] {
			if snap, ok := optBool(r, "snap"); ok && snap {
				q := url.Values{}
				q.Set("filter", "interactive")
				q.Set("format", "compact")
				if tabID := optString(r, "tabId"); tabID != "" {
					q.Set("tabId", tabID)
				}
				snapBody, _, snapErr := c.Get(ctx, "/snapshot", q)
				if snapErr == nil {
					return mcp.NewToolResultText(string(body) + "\n" + string(snapBody)), nil
				}
			}
		}

		return resultFromBytes(body, code)
	}
}

func handleKeyboardText(c *Client, kind string) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		text, err := r.RequireString("text")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		payload := map[string]any{"kind": kind, "text": text}
		if tabID := optString(r, "tabId"); tabID != "" {
			payload["tabId"] = tabID
		}
		body, code, err := c.Post(ctx, routedPathWithBody(r, "/action", payload), payload)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultFromBytes(body, code)
	}
}

func handleKeyboardKey(c *Client, kind string) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		key, err := r.RequireString("key")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		payload := map[string]any{"kind": kind, "key": key}
		if tabID := optString(r, "tabId"); tabID != "" {
			payload["tabId"] = tabID
		}
		body, code, err := c.Post(ctx, routedPathWithBody(r, "/action", payload), payload)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultFromBytes(body, code)
	}
}

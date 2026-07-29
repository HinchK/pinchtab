package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	bridgecdpops "github.com/pinchtab/pinchtab/internal/bridge/cdpops"
	"github.com/pinchtab/pinchtab/internal/selector"
)

// ErrSelectorNoMatch marks a resolution failure where the selector was valid
// but matched no element (a client-side "not found"), as distinct from a
// CDP/transport fault, an unsupported selector kind, or an internal routing
// error — which must surface as 5xx, not 404. Callers classify with errors.Is.
var ErrSelectorNoMatch = errors.New("selector matched no element")

// ErrSelectorOutsideScope marks a cached ref that still exists but belongs to
// the background document rather than the active modal subtree. Callers must
// not treat this as a stale ref or invoke global semantic recovery.
var ErrSelectorOutsideScope = errors.New("selector target is outside scope")

type FrameElementMeta struct {
	TagName string `json:"tagName"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Title   string `json:"title,omitempty"`
	Src     string `json:"src,omitempty"`
}

// FrameExecutionContextID returns a Runtime.executionContextId that
// evaluates in the given frame's document. Safe to call from other packages
// that need to scope `Runtime.evaluate` / `Runtime.callFunctionOn` to a
// frame (for example, the /text handler when a frame scope is active).
// Passes frameID == "" through as a no-op (returns 0, nil) so callers can
// fall back to the default top-level context without branching.
func FrameExecutionContextID(ctx context.Context, frameID string) (int64, error) {
	return frameExecutionContextID(ctx, frameID)
}

func frameExecutionContextID(ctx context.Context, frameID string) (int64, error) {
	return bridgecdpops.FrameExecutionContextID(ctx, frameID)
}

// isolatedExecutionContextID returns the isolated world's execution context for
// frameID, or for the top frame when frameID is empty. Selector and modal
// discovery run there so page script cannot hide or redirect targets by
// replacing DOM methods in the main world. It never returns a usable zero: a
// caller that cannot get an isolated context gets an error, not the main world.
func isolatedExecutionContextID(ctx context.Context, frameID string) (int64, error) {
	if frameID == "" {
		frameTree, err := FetchFrameTree(ctx)
		if err != nil {
			return 0, fmt.Errorf("resolve top frame: %w", err)
		}
		frameID = frameTree.Frame.ID
		if frameID == "" {
			return 0, fmt.Errorf("resolve top frame: frame id is empty")
		}
	}
	execID, err := frameExecutionContextID(ctx, frameID)
	if err != nil {
		return 0, err
	}
	if execID == 0 {
		return 0, fmt.Errorf("frame %q has no isolated execution context", frameID)
	}
	return execID, nil
}

// IsolatedNodeObjectID converts a backend node id to a JS object handle in the
// isolated world. DOM.resolveNode without an executionContextId hands back a
// main-world object, and every Runtime.callFunctionOn against it then runs where
// page script can redefine the DOM methods it uses — so a scoped selector or a
// clip origin could be steered by the page it is inspecting.
//
// The isolated world is per-frame but a handle from any frame's world reaches a
// node in another same-process frame, so the top frame's world is used rather
// than the node's own: a bare backend node id does not carry its frame, and
// DOM.describeNode reports frameId only for frame owner elements.
func IsolatedNodeObjectID(ctx context.Context, backendNodeID int64) (string, error) {
	execID, err := isolatedExecutionContextID(ctx, "")
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

func frameDocumentObjectID(ctx context.Context, frameID string) (string, error) {
	execID, err := isolatedExecutionContextID(ctx, frameID)
	if err != nil {
		return "", err
	}

	params := map[string]any{
		"expression":    "document",
		"returnByValue": false,
		"contextId":     execID,
	}

	var docResult json.RawMessage
	err = chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return chromedp.FromContext(ctx).Target.Execute(ctx, "Runtime.evaluate", params, &docResult)
	}))
	if err != nil {
		return "", fmt.Errorf("resolve document: %w", err)
	}

	var doc struct {
		Result struct {
			ObjectID string `json:"objectId"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails,omitempty"`
	}
	if err := json.Unmarshal(docResult, &doc); err != nil {
		return "", err
	}
	if doc.Result.ObjectID == "" {
		if doc.ExceptionDetails != nil {
			return "", fmt.Errorf("resolve document: %s", doc.ExceptionDetails.Text)
		}
		return "", fmt.Errorf("document object not found")
	}
	return doc.Result.ObjectID, nil
}

func backendNodeIDFromObjectID(ctx context.Context, objectID string) (int64, error) {
	var nodeResult json.RawMessage
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return chromedp.FromContext(ctx).Target.Execute(ctx, "DOM.requestNode", map[string]any{
			"objectId": objectID,
		}, &nodeResult)
	}))
	if err != nil {
		return 0, fmt.Errorf("request node: %w", err)
	}

	var node struct {
		NodeID int64 `json:"nodeId"`
	}
	if err := json.Unmarshal(nodeResult, &node); err != nil {
		return 0, err
	}
	if node.NodeID == 0 {
		return 0, fmt.Errorf("resolved to an invalid node")
	}

	var descResult json.RawMessage
	err = chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return chromedp.FromContext(ctx).Target.Execute(ctx, "DOM.describeNode", map[string]any{
			"nodeId": node.NodeID,
		}, &descResult)
	}))
	if err != nil {
		return 0, fmt.Errorf("describe node: %w", err)
	}

	var desc struct {
		Node struct {
			BackendNodeID int64 `json:"backendNodeId"`
		} `json:"node"`
	}
	if err := json.Unmarshal(descResult, &desc); err != nil {
		return 0, err
	}
	if desc.Node.BackendNodeID == 0 {
		return 0, fmt.Errorf("resolved to an invalid backend node")
	}
	return desc.Node.BackendNodeID, nil
}

func resolveNodeInFrame(ctx context.Context, frameID, functionDeclaration string, args []map[string]any) (int64, error) {
	docObjectID, err := frameDocumentObjectID(ctx, frameID)
	if err != nil {
		return 0, err
	}

	var callResult json.RawMessage
	err = chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return chromedp.FromContext(ctx).Target.Execute(ctx, "Runtime.callFunctionOn", map[string]any{
			"functionDeclaration": functionDeclaration,
			"objectId":            docObjectID,
			"arguments":           args,
			"returnByValue":       false,
		}, &callResult)
	}))
	if err != nil {
		return 0, err
	}

	var call struct {
		Result struct {
			Type     string `json:"type"`
			Subtype  string `json:"subtype"`
			ObjectID string `json:"objectId"`
		} `json:"result"`
	}
	if err := json.Unmarshal(callResult, &call); err != nil {
		return 0, err
	}
	if call.Result.ObjectID == "" || call.Result.Subtype == "null" || call.Result.Type == "undefined" {
		return 0, fmt.Errorf("%w", ErrSelectorNoMatch)
	}

	return backendNodeIDFromObjectID(ctx, call.Result.ObjectID)
}

// TopmostModalNodeIDInFrame returns the backend node ID of the visually
// topmost visible modal owner in a frame. A missing dialog is a normal result,
// not an error. Discovery runs in an isolated world and uses browser hit
// testing rather than comparing local z-index values, which are not comparable
// across stacking contexts or the native top layer.
func TopmostModalNodeIDInFrame(ctx context.Context, frameID string) (int64, bool, error) {
	const topmostModalFn = `function() {
		const candidates = [];
		const seen = new Set();
		const composedParent = (node) => node && (node.parentNode || node.host || null);
		const composedContains = (ancestor, node) => {
			for (let cur = node; cur; cur = composedParent(cur)) if (cur === ancestor) return true;
			return false;
		};
		const visit = (root) => {
			if (!root || !root.querySelectorAll) return;
			for (const el of root.querySelectorAll('dialog, [aria-modal="true"]')) {
				if (!seen.has(el)) { seen.add(el); candidates.push(el); }
			}
			for (const el of root.querySelectorAll("*")) if (el.shadowRoot) visit(el.shadowRoot);
		};
		visit(this);
		const visible = candidates.filter((el) => {
			if (!el || !el.isConnected) return false;
			let nativeModal = false;
			try { nativeModal = el.matches(":modal"); } catch (_) {}
			if (!nativeModal && el.getAttribute("aria-modal") !== "true") return false;
			for (let cur = el; cur; cur = composedParent(cur)) {
				if (cur.nodeType !== 1) continue;
				if (cur.getAttribute("aria-hidden") === "true") return false;
				const style = cur.ownerDocument.defaultView.getComputedStyle(cur);
				if (style.display === "none" || style.visibility === "hidden" || style.visibility === "collapse") return false;
				if (Number.parseFloat(style.opacity || "1") <= 0) return false;
			}
			const rect = el.getBoundingClientRect();
			return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 &&
				rect.left < el.ownerDocument.defaultView.innerWidth && rect.top < el.ownerDocument.defaultView.innerHeight;
		});
		if (!visible.length) return null;

		// An inner modal owns interaction before any containing modal, regardless
		// of the outer element's local z-index.
		const leaves = visible.filter((candidate) => !visible.some((other) =>
			other !== candidate && composedContains(candidate, other)));
		const points = [];
		const pointKeys = new Set();
		const addPoint = (x, y) => {
			x = Math.max(0, Math.min(this.defaultView.innerWidth - 1, x));
			y = Math.max(0, Math.min(this.defaultView.innerHeight - 1, y));
			const key = Math.round(x) + ":" + Math.round(y);
			if (!pointKeys.has(key)) { pointKeys.add(key); points.push([x, y]); }
		};
		for (const el of leaves) {
			const r = el.getBoundingClientRect();
			const left = Math.max(0, r.left), right = Math.min(this.defaultView.innerWidth, r.right);
			const top = Math.max(0, r.top), bottom = Math.min(this.defaultView.innerHeight, r.bottom);
			addPoint((left + right) / 2, (top + bottom) / 2);
			addPoint(left + (right - left) * .2, top + (bottom - top) * .2);
			addPoint(right - (right - left) * .2, top + (bottom - top) * .2);
			addPoint(left + (right - left) * .2, bottom - (bottom - top) * .2);
			addPoint(right - (right - left) * .2, bottom - (bottom - top) * .2);
		}
		const deepHit = (x, y) => {
			let root = this, hit = null;
			while (root && root.elementFromPoint) {
				const next = root.elementFromPoint(x, y);
				if (!next || next === hit) break;
				hit = next;
				root = next.shadowRoot;
			}
			return hit;
		};
		let best = null, bestHits = 0;
		for (const candidate of leaves) {
			let hits = 0;
			for (const [x, y] of points) if (composedContains(candidate, deepHit(x, y))) hits++;
			if (hits > bestHits || (hits === bestHits && hits > 0)) {
				best = candidate;
				bestHits = hits;
			}
		}
		return bestHits > 0 ? best : null;
	}`

	nodeID, err := resolveNodeInFrame(ctx, frameID, topmostModalFn, nil)
	if errors.Is(err, ErrSelectorNoMatch) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("resolve topmost dialog: %w", err)
	}
	return nodeID, true, nil
}

// TopmostModalNodeID gives a top-document modal precedence over a caller's
// current iframe scope. This prevents a stale frame scope from interacting
// with background content underneath a page-level modal. When the top
// document has no modal, it falls back to the requested frame.
func TopmostModalNodeID(ctx context.Context, frameID string) (int64, bool, error) {
	nodeID, open, err := TopmostModalNodeIDInFrame(ctx, "")
	if err != nil || open || frameID == "" {
		return nodeID, open, err
	}
	return TopmostModalNodeIDInFrame(ctx, frameID)
}

// resolveNodeWithinBackendNode invokes functionDeclaration with the scope
// element as `this` and converts the returned DOM object to a backend node ID.
func resolveNodeWithinBackendNode(ctx context.Context, scopeBackendNodeID int64, functionDeclaration string, args []map[string]any) (int64, error) {
	scopeObjectID, err := IsolatedNodeObjectID(ctx, scopeBackendNodeID)
	if err != nil {
		return 0, fmt.Errorf("resolve scope node: %w", err)
	}

	params := map[string]any{
		"functionDeclaration": functionDeclaration,
		"objectId":            scopeObjectID,
		"returnByValue":       false,
	}
	if len(args) > 0 {
		params["arguments"] = args
	}

	var callResult json.RawMessage
	err = chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return chromedp.FromContext(ctx).Target.Execute(ctx, "Runtime.callFunctionOn", params, &callResult)
	}))
	if err != nil {
		return 0, err
	}

	var call struct {
		Result struct {
			Type     string `json:"type"`
			Subtype  string `json:"subtype"`
			ObjectID string `json:"objectId"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails,omitempty"`
	}
	if err := json.Unmarshal(callResult, &call); err != nil {
		return 0, err
	}
	if call.ExceptionDetails != nil {
		return 0, fmt.Errorf("scoped selector evaluation: %s", call.ExceptionDetails.Text)
	}
	if call.Result.ObjectID == "" || call.Result.Subtype == "null" || call.Result.Type == "undefined" {
		return 0, fmt.Errorf("%w", ErrSelectorNoMatch)
	}
	return backendNodeIDFromObjectID(ctx, call.Result.ObjectID)
}

// BackendNodeWithinScope reports whether target is the scope node or one of
// its DOM descendants. It is used to reject stale/background snapshot refs
// while a modal dialog owns the interaction surface.
func BackendNodeWithinScope(ctx context.Context, scopeBackendNodeID, targetBackendNodeID int64) (bool, error) {
	if scopeBackendNodeID == 0 || targetBackendNodeID == 0 {
		return false, nil
	}

	scopeObjectID, err := IsolatedNodeObjectID(ctx, scopeBackendNodeID)
	if err != nil {
		return false, fmt.Errorf("resolve scope node: %w", err)
	}
	targetObjectID, err := IsolatedNodeObjectID(ctx, targetBackendNodeID)
	if err != nil {
		return false, fmt.Errorf("resolve target node: %w", err)
	}

	var contains bool
	err = chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var raw json.RawMessage
		if err := chromedp.FromContext(ctx).Target.Execute(ctx, "Runtime.callFunctionOn", map[string]any{
			"functionDeclaration": `function(target) {
				for (let cur = target; cur; cur = cur.parentNode || cur.host || null) {
					if (cur === this) return true;
				}
				return false;
			}`,
			"objectId":      scopeObjectID,
			"arguments":     []map[string]any{{"objectId": targetObjectID}},
			"returnByValue": true,
		}, &raw); err != nil {
			return err
		}
		var parsed struct {
			Result struct {
				Value bool `json:"value"`
			} `json:"result"`
			ExceptionDetails *struct {
				Text string `json:"text"`
			} `json:"exceptionDetails,omitempty"`
		}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return err
		}
		if parsed.ExceptionDetails != nil {
			return fmt.Errorf("scope containment check: %s", parsed.ExceptionDetails.Text)
		}
		contains = parsed.Result.Value
		return nil
	}))
	return contains, err
}

func resolveElementMetaInFrame(ctx context.Context, frameID, functionDeclaration string, args []map[string]any) (FrameElementMeta, error) {
	docObjectID, err := frameDocumentObjectID(ctx, frameID)
	if err != nil {
		return FrameElementMeta{}, err
	}

	var callResult json.RawMessage
	err = chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return chromedp.FromContext(ctx).Target.Execute(ctx, "Runtime.callFunctionOn", map[string]any{
			"functionDeclaration": functionDeclaration,
			"objectId":            docObjectID,
			"arguments":           args,
			"returnByValue":       true,
		}, &callResult)
	}))
	if err != nil {
		return FrameElementMeta{}, err
	}

	var call struct {
		Result struct {
			Type    string          `json:"type"`
			Subtype string          `json:"subtype"`
			Value   json.RawMessage `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(callResult, &call); err != nil {
		return FrameElementMeta{}, err
	}
	if call.Result.Subtype == "null" || call.Result.Type == "undefined" || len(call.Result.Value) == 0 || string(call.Result.Value) == "null" {
		return FrameElementMeta{}, fmt.Errorf("no element found")
	}

	var meta FrameElementMeta
	if err := json.Unmarshal(call.Result.Value, &meta); err != nil {
		return FrameElementMeta{}, err
	}
	meta.TagName = strings.ToLower(meta.TagName)
	return meta, nil
}

// jsNormalizeHelper lowercases, collapses runs of whitespace to a single space,
// and trims. Indentation here is cosmetic (JS ignores it); only the runtime
// behavior matters.
const jsNormalizeHelper = `const normalize = (value) => String(value || "")
		.toLowerCase()
		.replace(/\s+/g, " ")
		.trim();`

func ResolveTextToNodeID(ctx context.Context, text string) (int64, error) {
	return ResolveTextToNodeIDInFrame(ctx, "", text)
}

func ResolveTextToNodeIDInFrame(ctx context.Context, frameID, text string) (int64, error) {
	return resolveSelectorAtInFrame(ctx, frameID, selector.Selector{Kind: selector.KindText, Value: text}, 0, false)
}

const resolveSelectorAtFn = `function(kind, value, index, fromEnd) {
	const root = this;
	` + jsNormalizeHelper + `
	const needle = normalize(value);
	const unique = (items) => {
		const seen = new Set();
		const out = [];
		for (const item of items) {
			if (!item || seen.has(item)) continue;
			seen.add(item);
			out.push(item);
		}
		return out;
	};
	const pick = (items) => {
		items = unique(items);
		if (!items.length) return null;
		const idx = fromEnd ? items.length - 1 : index;
		if (idx < 0 || idx >= items.length) return null;
		return items[idx];
	};
	const deepQueryAll = (selector, from) => {
		const out = [];
		const visit = (scope) => {
			if (!scope || !scope.querySelectorAll) return;
			if (scope.nodeType === 1 && scope.matches && scope.matches(selector)) out.push(scope);
			const elements = Array.from(scope.querySelectorAll("*"));
			out.push(...Array.from(scope.querySelectorAll(selector)));
			for (const el of elements) if (el.shadowRoot) visit(el.shadowRoot);
		};
		visit(from || root);
		return out;
	};
	// Source text is not rendered text: a CSS comment or a script string
	// mentioning a label is nothing a user can see or click, so matching it
	// hands back an element no action can operate on.
	const nonRendered = new Set(["script", "style", "noscript", "template"]);
	// At equal subtree size a control outranks a plain wrapper carrying the same
	// label, so text: lands on the <button> rather than the <div> around it.
	const semanticWeight = (el) => {
		const tag = (el.tagName || "").toLowerCase();
		if (tag === "button" || tag === "a" || tag === "input") return 0.25;
		const role = normalize(el.getAttribute && el.getAttribute("role"));
		if (role === "button" || role === "link" || role === "textbox") return 0.2;
		return 0;
	};
	// Leaf-most wins: the fewest descendants is the most specific match. The sort
	// is stable, so equally-weighted candidates keep document order and the index
	// first/last/nth asks for stays meaningful.
	const smallestFirst = (matches) => {
		const minSize = Math.min(...matches.map((item) => item.size));
		return matches
			.filter((item) => item.size === minSize)
			.sort((a, b) => semanticWeight(b.el) - semanticWeight(a.el))
			.map((item) => item.el);
	};
	const textCandidates = (query) => {
		if (!query) return [];
		// textContent, not innerText: innerText forces a synchronous layout per
		// element and is O(N^2) on large pages.
		const elements = deepQueryAll("*", root.body || root)
			.filter((el) => !nonRendered.has((el.tagName || "").toLowerCase()));
		const measured = elements.map((el) => ({ el, text: normalize(el.textContent || ""), size: el.getElementsByTagName("*").length }));
		const exact = measured.filter((item) => item.text && item.text.includes(query));
		if (exact.length) return smallestFirst(exact);
		const tokens = query.split(" ").filter(Boolean);
		if (!tokens.length) return [];
		const fuzzy = measured.filter((item) => {
			if (!item.text) return false;
			let hits = 0;
			for (const token of tokens) if (item.text.includes(token)) hits++;
			return hits / tokens.length >= 0.7;
		});
		if (!fuzzy.length) return [];
		return smallestFirst(fuzzy);
	};

	try {
		switch (kind) {
		case "css":
			return pick(deepQueryAll(value));
		case "xpath": {
			const document = root.ownerDocument || root;
			const result = document.evaluate(value, root, null, XPathResult.ORDERED_NODE_SNAPSHOT_TYPE, null);
			const items = [];
			for (let i = 0; i < result.snapshotLength; i++) {
				const item = result.snapshotItem(i);
				if (root.nodeType === 9 || item === root || (root.contains && root.contains(item))) items.push(item);
			}
			return pick(items);
		}
		case "text":
			return pick(textCandidates(needle));
		default:
			return null;
		}
	} catch (e) {
		return null;
	}
}`

// textLookupDeadline bounds the text scan so a slow resolution cannot eat the
// whole action timeout. It applies wherever text is matched, wrapped or not —
// first:/last:/nth: reach the same scan.
const textLookupDeadline = 3 * time.Second

// resolveSelectorAt is the single entry point for every css/xpath/text
// resolution. scopeLabel names the scope in errors; call runs the resolver
// against whichever root the caller resolved.
func resolveSelectorAt(ctx context.Context, sel selector.Selector, index int, fromEnd bool, scopeLabel string, call func(context.Context, []map[string]any) (int64, error)) (int64, error) {
	switch sel.Kind {
	case selector.KindCSS, selector.KindXPath, selector.KindText:
	default:
		return 0, fmt.Errorf("%s selector cannot be used with first/last/nth", sel.Kind)
	}

	lookupCtx := ctx
	if sel.Kind == selector.KindText {
		var cancel context.CancelFunc
		lookupCtx, cancel = context.WithTimeout(ctx, textLookupDeadline)
		defer cancel()
	}

	nid, err := call(lookupCtx, []map[string]any{
		{"value": string(sel.Kind)},
		{"value": sel.Value},
		{"value": index},
		{"value": fromEnd},
	})
	if err != nil {
		if lookupCtx.Err() != nil && ctx.Err() == nil {
			return 0, fmt.Errorf("%s %q%s lookup timed out after %s (page may be large or unresponsive): %w", sel.Kind, sel.Value, scopeLabel, textLookupDeadline, err)
		}
		return 0, fmt.Errorf("%s %q%s: %w", sel.Kind, sel.Value, scopeLabel, err)
	}
	return nid, nil
}

func resolveSelectorAtInFrame(ctx context.Context, frameID string, sel selector.Selector, index int, fromEnd bool) (int64, error) {
	return resolveSelectorAt(ctx, sel, index, fromEnd, "", func(ctx context.Context, args []map[string]any) (int64, error) {
		var backendNodeID int64
		err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			nid, err := resolveNodeInFrame(ctx, frameID, resolveSelectorAtFn, args)
			if err != nil {
				return err
			}
			backendNodeID = nid
			return nil
		}))
		return backendNodeID, err
	})
}

func resolveSelectorAtWithinNode(ctx context.Context, scopeBackendNodeID int64, sel selector.Selector, index int, fromEnd bool) (int64, error) {
	return resolveSelectorAt(ctx, sel, index, fromEnd, " inside topmost dialog", func(ctx context.Context, args []map[string]any) (int64, error) {
		return resolveNodeWithinBackendNode(ctx, scopeBackendNodeID, resolveSelectorAtFn, args)
	})
}

func resolveNestedSelectorAtInFrame(ctx context.Context, frameID string, raw string, refCache *RefCache, index int, fromEnd bool) (int64, error) {
	inner := selector.Parse(raw)
	switch inner.Kind {
	case selector.KindFirst:
		return resolveNestedSelectorAtInFrame(ctx, frameID, inner.Value, refCache, 0, false)
	case selector.KindLast:
		return resolveNestedSelectorAtInFrame(ctx, frameID, inner.Value, refCache, 0, true)
	case selector.KindNth:
		nth, nestedRaw, err := selector.ParseNth(inner.Value)
		if err != nil {
			return 0, err
		}
		return resolveNestedSelectorAtInFrame(ctx, frameID, nestedRaw, refCache, nth, false)
	case selector.KindRef:
		if fromEnd || index != 0 {
			return 0, fmt.Errorf("ref selector cannot be used with last/nth")
		}
		return ResolveUnifiedSelectorInFrame(ctx, inner, refCache, frameID)
	case selector.KindSemantic:
		return 0, fmt.Errorf("semantic selectors must be resolved at the handler layer via /find")
	default:
		return resolveSelectorAtInFrame(ctx, frameID, inner, index, fromEnd)
	}
}

func resolveNestedSelectorWithinNode(ctx context.Context, scopeBackendNodeID int64, raw string, refCache *RefCache, index int, fromEnd bool) (int64, error) {
	inner := selector.Parse(raw)
	switch inner.Kind {
	case selector.KindFirst:
		return resolveNestedSelectorWithinNode(ctx, scopeBackendNodeID, inner.Value, refCache, 0, false)
	case selector.KindLast:
		return resolveNestedSelectorWithinNode(ctx, scopeBackendNodeID, inner.Value, refCache, 0, true)
	case selector.KindNth:
		nth, nestedRaw, err := selector.ParseNth(inner.Value)
		if err != nil {
			return 0, err
		}
		return resolveNestedSelectorWithinNode(ctx, scopeBackendNodeID, nestedRaw, refCache, nth, false)
	case selector.KindRef:
		if fromEnd || index != 0 {
			return 0, fmt.Errorf("ref selector cannot be used with last/nth")
		}
		return ResolveUnifiedSelectorWithinNode(ctx, inner, refCache, scopeBackendNodeID)
	case selector.KindSemantic:
		return 0, fmt.Errorf("semantic selectors must be resolved at the handler layer via /find")
	default:
		return resolveSelectorAtWithinNode(ctx, scopeBackendNodeID, inner, index, fromEnd)
	}
}

func ResolveXPathToNodeID(ctx context.Context, xpath string) (int64, error) {
	return ResolveXPathToNodeIDInFrame(ctx, "", xpath)
}

func ResolveCSSToNodeID(ctx context.Context, css string) (int64, error) {
	return ResolveCSSToNodeIDInFrame(ctx, "", css)
}

func ResolveCSSToNodeIDInFrame(ctx context.Context, frameID, css string) (int64, error) {
	// Resolve through the deep selector walker (resolveSelectorAtFn → deepQueryAll)
	// so a CSS selector matches the first element even when it is nested in an open
	// shadow root, not just the light DOM (issue #591). For light-DOM pages this
	// returns the same first match as document.querySelector, and it works for both
	// the main frame (frameID == "") and sub-frames.
	return resolveSelectorAtInFrame(ctx, frameID, selector.Selector{Kind: selector.KindCSS, Value: css}, 0, false)
}

// XPath does not pierce shadow roots: document.evaluate cannot cross a shadow
// boundary. It resolves through the shared walker for consistency and
// isolated-world execution, not for shadow support.
func ResolveXPathToNodeIDInFrame(ctx context.Context, frameID, xpath string) (int64, error) {
	return resolveSelectorAtInFrame(ctx, frameID, selector.Selector{Kind: selector.KindXPath, Value: xpath}, 0, false)
}

func ResolveFrameElementMetaInFrame(ctx context.Context, sel selector.Selector, frameID string) (FrameElementMeta, error) {
	switch sel.Kind {
	case selector.KindCSS:
		return resolveElementMetaInFrame(ctx, frameID, `function(selector) {
			const el = this.querySelector(selector);
			if (!el) {
				return null;
			}
			return {
				tagName: (el.tagName || "").toLowerCase(),
				id: el.id || "",
				name: el.getAttribute("name") || "",
				title: el.getAttribute("title") || "",
				src: el.src || ""
			};
		}`, []map[string]any{{"value": sel.Value}})
	case selector.KindXPath:
		return resolveElementMetaInFrame(ctx, frameID, `function(xpath) {
			const el = this.evaluate(xpath, this, null, XPathResult.FIRST_ORDERED_NODE_TYPE, null).singleNodeValue;
			if (!el) {
				return null;
			}
			return {
				tagName: (el.tagName || "").toLowerCase(),
				id: el.id || "",
				name: el.getAttribute && el.getAttribute("name") || "",
				title: el.getAttribute && el.getAttribute("title") || "",
				src: el.src || ""
			};
		}`, []map[string]any{{"value": sel.Value}})
	default:
		return FrameElementMeta{}, fmt.Errorf("frame element metadata requires css or xpath selector")
	}
}

// ResolveUnifiedSelectorInFrame resolves a parsed selector to a backend node ID.
// Ref selectors still use the ref cache directly; non-ref selectors honor the
// provided frame scope.
func ResolveUnifiedSelectorInFrame(ctx context.Context, sel selector.Selector, refCache *RefCache, frameID string) (int64, error) {
	switch sel.Kind {
	case selector.KindRef:
		if refCache != nil {
			if target, ok := refCache.Lookup(sel.Value); ok {
				return target.BackendNodeID, nil
			}
		}
		return 0, fmt.Errorf("ref %s not in snapshot cache: %w", sel.Value, ErrSelectorNoMatch)

	case selector.KindCSS:
		return ResolveCSSToNodeIDInFrame(ctx, frameID, sel.Value)

	case selector.KindXPath:
		return ResolveXPathToNodeIDInFrame(ctx, frameID, sel.Value)

	case selector.KindText:
		return ResolveTextToNodeIDInFrame(ctx, frameID, sel.Value)

	case selector.KindSemantic:
		return 0, fmt.Errorf("semantic selectors must be resolved at the handler layer via /find")

	case selector.KindRole, selector.KindLabel, selector.KindPlaceholder,
		selector.KindAlt, selector.KindTitle, selector.KindTestID:
		return 0, fmt.Errorf("%s selectors must be resolved at the handler layer via semantic", sel.Kind)

	case selector.KindFirst:
		return resolveNestedSelectorAtInFrame(ctx, frameID, sel.Value, refCache, 0, false)

	case selector.KindLast:
		return resolveNestedSelectorAtInFrame(ctx, frameID, sel.Value, refCache, 0, true)

	case selector.KindNth:
		index, rawSelector, err := selector.ParseNth(sel.Value)
		if err != nil {
			return 0, err
		}
		return resolveNestedSelectorAtInFrame(ctx, frameID, rawSelector, refCache, index, false)

	default:
		return 0, fmt.Errorf("unknown selector kind: %q", sel.Kind)
	}
}

// ResolveUnifiedSelectorWithinNode resolves a selector strictly inside a DOM
// subtree. Ref selectors are accepted only when their cached backend node is
// still contained by that subtree; all other supported selectors are
// evaluated with the scope element as their root.
func ResolveUnifiedSelectorWithinNode(ctx context.Context, sel selector.Selector, refCache *RefCache, scopeBackendNodeID int64) (int64, error) {
	if scopeBackendNodeID == 0 {
		return 0, fmt.Errorf("dialog scope is missing")
	}

	switch sel.Kind {
	case selector.KindRef:
		if refCache == nil {
			return 0, fmt.Errorf("ref %s not in snapshot cache: %w", sel.Value, ErrSelectorNoMatch)
		}
		target, ok := refCache.Lookup(sel.Value)
		if !ok || target.BackendNodeID == 0 {
			return 0, fmt.Errorf("ref %s not in snapshot cache: %w", sel.Value, ErrSelectorNoMatch)
		}
		inside, err := BackendNodeWithinScope(ctx, scopeBackendNodeID, target.BackendNodeID)
		if err != nil {
			return 0, fmt.Errorf("validate ref %s against topmost dialog: %w", sel.Value, err)
		}
		if !inside {
			return 0, fmt.Errorf("ref %s is outside the topmost dialog: %w", sel.Value, ErrSelectorOutsideScope)
		}
		return target.BackendNodeID, nil

	case selector.KindCSS, selector.KindXPath, selector.KindText:
		return resolveSelectorAtWithinNode(ctx, scopeBackendNodeID, sel, 0, false)

	case selector.KindSemantic:
		return 0, fmt.Errorf("semantic selectors must be resolved at the handler layer via /find")

	case selector.KindRole, selector.KindLabel, selector.KindPlaceholder,
		selector.KindAlt, selector.KindTitle, selector.KindTestID:
		return 0, fmt.Errorf("%s selectors must be resolved at the handler layer via semantic", sel.Kind)

	case selector.KindFirst:
		return resolveNestedSelectorWithinNode(ctx, scopeBackendNodeID, sel.Value, refCache, 0, false)

	case selector.KindLast:
		return resolveNestedSelectorWithinNode(ctx, scopeBackendNodeID, sel.Value, refCache, 0, true)

	case selector.KindNth:
		index, rawSelector, err := selector.ParseNth(sel.Value)
		if err != nil {
			return 0, err
		}
		return resolveNestedSelectorWithinNode(ctx, scopeBackendNodeID, rawSelector, refCache, index, false)

	default:
		return 0, fmt.Errorf("unknown selector kind: %q", sel.Kind)
	}
}

func ResolveUnifiedSelector(ctx context.Context, sel selector.Selector, refCache *RefCache) (int64, error) {
	return ResolveUnifiedSelectorInFrame(ctx, sel, refCache, "")
}

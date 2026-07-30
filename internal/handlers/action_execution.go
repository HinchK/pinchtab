package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/config"
	"github.com/pinchtab/pinchtab/internal/selector"
	"github.com/pinchtab/semantic"
	"github.com/pinchtab/semantic/recovery"
)

const (
	postSubmitPollTimeout  = 3 * time.Second
	postSubmitPollInterval = 250 * time.Millisecond
)

var readTopmostSubmitModal = bridge.TopmostModalNodeIDInFrame

type submitStateSnapshot struct {
	URL        string `json:"url"`
	DialogOpen bool   `json:"dialogOpen"`
}

type submitPostState struct {
	Status         string              `json:"status"`
	Signal         string              `json:"signal"`
	Dispatch       string              `json:"dispatch"`
	ActionTimedOut bool                `json:"actionTimedOut"`
	ElapsedMS      int64               `json:"elapsedMs"`
	Before         submitStateSnapshot `json:"before"`
	After          submitStateSnapshot `json:"after"`
}

func (h *Handlers) captureSubmitState(ctx context.Context, tabID string) (submitStateSnapshot, error) {
	currentURL, err := h.Bridge.CurrentURL(ctx)
	if err != nil {
		return submitStateSnapshot{}, fmt.Errorf("read current URL: %w", err)
	}
	_, dialogOpen, err := readTopmostSubmitModal(ctx, h.selectorFrameID(tabID))
	if err != nil {
		return submitStateSnapshot{}, fmt.Errorf("read topmost dialog: %w", err)
	}
	return submitStateSnapshot{
		URL:        strings.TrimSpace(currentURL),
		DialogOpen: dialogOpen,
	}, nil
}

func submitTransitionSignal(before, after submitStateSnapshot) string {
	if before.URL != after.URL {
		return "url_changed"
	}
	if before.DialogOpen && !after.DialogOpen {
		return "dialog_closed"
	}
	return ""
}

func pendingSubmitPostState(before, after submitStateSnapshot, dispatch string, actionTimedOut bool, started time.Time) submitPostState {
	return submitPostState{
		Status:         "pending",
		Signal:         "no_terminal_change",
		Dispatch:       dispatch,
		ActionTimedOut: actionTimedOut,
		ElapsedMS:      time.Since(started).Milliseconds(),
		Before:         before,
		After:          after,
	}
}

// pollSubmitPostState observes only conservative terminal signals. The caller
// supplies a fresh bounded child of the live tab context, never the action
// context that may already have reached its deadline.
func (h *Handlers) pollSubmitPostState(ctx context.Context, tabID string, before submitStateSnapshot, dispatch string, actionTimedOut bool) (submitPostState, error) {
	started := time.Now()
	after := before
	observedPostState := false
	var lastReadErr error
	ticker := time.NewTicker(postSubmitPollInterval)
	defer ticker.Stop()

	for {
		observed, err := h.captureSubmitState(ctx, tabID)
		if err == nil {
			observedPostState = true
			lastReadErr = nil
			after = observed
			if signal := submitTransitionSignal(before, after); signal != "" {
				return submitPostState{
					Status:         "succeeded",
					Signal:         signal,
					Dispatch:       dispatch,
					ActionTimedOut: actionTimedOut,
					ElapsedMS:      time.Since(started).Milliseconds(),
					Before:         before,
					After:          after,
				}, nil
			}
		} else {
			lastReadErr = err
		}

		select {
		case <-ctx.Done():
			if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return submitPostState{}, fmt.Errorf("post-submit observation canceled: %w", ctx.Err())
			}
			if !observedPostState {
				if lastReadErr == nil {
					lastReadErr = ctx.Err()
				}
				return submitPostState{}, fmt.Errorf("post-submit state was never observable: %w", lastReadErr)
			}
			if lastReadErr != nil && !errors.Is(lastReadErr, context.DeadlineExceeded) {
				return submitPostState{}, fmt.Errorf("post-submit observation failed: %w", lastReadErr)
			}
			return pendingSubmitPostState(before, after, dispatch, actionTimedOut, started), nil
		case <-ticker.C:
		}
	}
}

func (h *Handlers) cacheActionIntent(tabID string, req bridge.ActionRequest) {
	if h.Recovery == nil || req.Ref == "" {
		return
	}
	// Don't overwrite an existing entry that has a real Query (from /find)
	// with a descriptor-only entry.
	if existing, ok := h.Recovery.IntentCache.Lookup(tabID, req.Ref); ok && existing.Query != "" {
		return
	}
	desc := semantic.ElementDescriptor{Ref: req.Ref}
	if cache := h.Bridge.GetRefCache(tabID); cache != nil {
		for i := range cache.Nodes {
			if cache.Nodes[i].Ref == req.Ref {
				desc = descriptorFromNode(cache.Nodes[i])
				break
			}
		}
	}
	h.Recovery.RecordIntent(tabID, req.Ref, recovery.IntentEntry{
		Descriptor: desc,
		CachedAt:   time.Now(),
	})
}

func (h *Handlers) executeAction(ctx context.Context, req bridge.ActionRequest, cfg *config.RuntimeConfig) (map[string]any, string, error) {
	req.Kind = bridge.CanonicalActionKind(req.Kind)

	if err := h.ensureBrowser(cfg); err != nil {
		return nil, "", fmt.Errorf("browser initialization: %w", err)
	}
	result, err := h.Bridge.ExecuteAction(ctx, req.Kind, req)
	return result, "", err
}

// executeActionResilient runs one resolved action with pointer-retry, stale-ref
// cache refresh, and semantic self-healing. When refMissing is set it goes
// recovery-first (Recovery.Attempt); otherwise it executes then heals on
// failure. Callers must handle the refMissing-without-recovery case (404 /
// per-step failure) before calling — refMissing=true assumes h.Recovery != nil.
// req.NodeID is mutated in place, as the inline action/batch/macro paths did.
func (h *Handlers) executeActionResilient(ctx context.Context, req *bridge.ActionRequest, cfg *config.RuntimeConfig, resolvedTabID string, refMissing bool) (map[string]any, string, *recovery.RecoveryResult, error) {
	submitClick := bridge.IsSubmitClick(req.Kind, *req)
	if refMissing {
		if submitClick {
			return nil, "", nil, fmt.Errorf("click submit target is unresolved; automatic recovery is disabled")
		}
		rr, recRes, recErr := h.Recovery.Attempt(
			ctx, resolvedTabID, req.Ref, req.Kind,
			func(c context.Context, _ string, nodeID int64) (map[string]any, error) {
				req.NodeID = nodeID
				if secErr := h.refreshActionSecondaryTargets(c, resolvedTabID, req); secErr != nil {
					return nil, secErr
				}
				res, _, err := h.executeAction(c, *req, cfg)
				return res, err
			},
		)
		if recErr != nil {
			return nil, "", &rr, fmt.Errorf("ref %s not found and recovery failed: %w", req.Ref, recErr)
		}
		return recRes, "", &rr, nil
	}

	result, backend, err := h.executeAction(ctx, *req, cfg)
	if err != nil && !submitClick && shouldRetryPointerAction(*req, err) {
		if (req.Ref != "" || req.ToSelector != "") && shouldRetryStaleRef(err) {
			recordStaleRefRetry()
			h.refreshRefCache(ctx, resolvedTabID)
			if cache := h.Bridge.GetRefCache(resolvedTabID); cache != nil && req.Ref != "" {
				if target, ok := cache.Lookup(req.Ref); ok {
					req.NodeID = target.BackendNodeID
				}
			}
		}
		h.refreshActionNodeIDFromSelector(ctx, req)
		if secErr := h.refreshActionSecondaryTargets(ctx, resolvedTabID, req); secErr != nil {
			err = secErr
		} else {
			time.Sleep(pointerRetryDelay)
			result, backend, err = h.executeAction(ctx, *req, cfg)
		}
	}

	var rr *recovery.RecoveryResult
	if err != nil && !submitClick && req.Ref != "" && h.Recovery != nil && h.Recovery.ShouldAttempt(err, req.Ref) {
		r2, recRes, recErr := h.Recovery.AttemptWithClassification(
			ctx, resolvedTabID, req.Ref, req.Kind,
			recovery.ClassifyFailure(err),
			func(c context.Context, _ string, nodeID int64) (map[string]any, error) {
				req.NodeID = nodeID
				if secErr := h.refreshActionSecondaryTargets(c, resolvedTabID, req); secErr != nil {
					return nil, secErr
				}
				res, _, e := h.executeAction(c, *req, cfg)
				return res, e
			},
		)
		rr = &r2
		if recErr == nil {
			result, err = recRes, nil
		}
	}
	return result, backend, rr, err
}

func switchedTabFromActionResult(result map[string]any) string {
	if switched, ok := result["switchedToTab"].(string); ok {
		return strings.TrimSpace(switched)
	}
	return ""
}

const pointerRetryDelay = 50 * time.Millisecond

func shouldRetryPointerAction(req bridge.ActionRequest, err error) bool {
	if err == nil {
		return false
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	switch kind {
	case bridge.ActionClick, bridge.ActionDoubleClick, bridge.ActionHover, bridge.ActionDrag,
		bridge.ActionMouseDown, bridge.ActionMouseUp, bridge.ActionMouseWheel:
	default:
		return false
	}

	if errors.Is(err, bridge.ErrElementOccluded) ||
		errors.Is(err, bridge.ErrElementBlocked) ||
		errors.Is(err, bridge.ErrElementOffscreen) {
		return true
	}

	return shouldRetryStaleRef(err)
}

// refreshActionSecondaryTargets re-resolves every selector-bearing field the request
// carries BEYOND the primary target, against the current snapshot. The primary is
// re-resolved by the caller (recovery's descriptor match, or the ref-cache lookup on
// the pointer retry); everything else must be re-derived rather than carried over,
// because a node id resolved before a refresh can denote a node the DOM change
// destroyed. Today the only secondary field is the drag destination; a future
// selector-bearing field is re-resolved by adding it HERE, not at each site — the
// census in action_drag_destination_test.go pins that every re-resolution site calls
// this.
//
// A destination ref that no longer resolves is re-found by EXACT identity — the
// unique node in the refreshed snapshot with the intent-cached descriptor's role and
// name — never by the fuzzy heal the source gets. Refs are positional per snapshot,
// so an ordinal re-lookup can denote a different element; and a drop is positional
// too, so a fuzzy lookalike match would land the payload somewhere the caller never
// named while reporting dragged:true. Measured: the default 0.4 match confidence
// happily "recovers" a deleted destination onto its sibling. Whether the source's
// fuzzy heal should report what it did is a separate, pending question; nothing here
// classifies recovery outcomes.
func (h *Handlers) refreshActionSecondaryTargets(ctx context.Context, tabID string, req *bridge.ActionRequest) error {
	toSelector := strings.TrimSpace(req.ToSelector)
	if toSelector == "" {
		return nil
	}
	req.ToNodeID = 0
	if resolution, err := h.resolveActionRequestDestination(ctx, tabID, req); err == nil && !resolution.refMissing && req.ToNodeID != 0 {
		return nil
	}
	req.ToNodeID = 0
	if h.Recovery != nil {
		if sel := selector.Parse(toSelector); sel.Kind == selector.KindRef {
			if entry, ok := h.Recovery.IntentCache.Lookup(tabID, sel.Value); ok {
				if nid, found := uniqueNodeForDescriptor(h.Bridge.GetRefCache(tabID), entry.Descriptor); found {
					req.ToNodeID = nid
					return nil
				}
			}
		}
	}
	return fmt.Errorf("drag destination %s not found - take a /snapshot first", toSelector)
}

// uniqueNodeForDescriptor returns the one node matching the descriptor's role and
// name exactly, and refuses when zero or several do — an ambiguous destination is a
// coin flip over where the payload lands.
func uniqueNodeForDescriptor(cache *bridge.RefCache, desc semantic.ElementDescriptor) (int64, bool) {
	if cache == nil || (desc.Role == "" && desc.Name == "") {
		return 0, false
	}
	var match int64
	count := 0
	for i := range cache.Nodes {
		node := cache.Nodes[i]
		if node.Role != desc.Role || node.Name != desc.Name || node.NodeID == 0 {
			continue
		}
		match = node.NodeID
		count++
	}
	if count != 1 {
		return 0, false
	}
	return match, true
}

func (h *Handlers) refreshActionNodeIDFromSelector(ctx context.Context, req *bridge.ActionRequest) {
	if req == nil || req.NodeID > 0 || strings.TrimSpace(req.Selector) == "" {
		return
	}
	nid, err := bridge.ResolveCSSToNodeID(ctx, req.Selector)
	if err != nil {
		return
	}
	req.NodeID = nid
}

func shouldRetryStaleRef(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, bridge.ErrElementStale) {
		return true
	}
	// Fallback string matching is still needed for stale failures that can bypass
	// bridge.ExecuteAction classification (for example, static paths or other
	// non-bridge error surfaces that return raw backend-node messages).
	// "is detached" is CDP's spelling for a node that was removed and re-created —
	// measured from a drag against a re-rendered list ("Node is detached from
	// document (-32000)") — and without it neither the retry nor recovery ever ran
	// for exactly the DOM change stale-ref recovery exists for.
	e := strings.ToLower(err.Error())
	return strings.Contains(e, "could not find node") || strings.Contains(e, "node with given id") ||
		strings.Contains(e, "no node") || strings.Contains(e, "is detached")
}

func (h *Handlers) refreshRefCache(ctx context.Context, tabID string) {
	nodes, err := bridge.FetchAXTree(ctx)
	if err != nil {
		return
	}
	flat, refs := bridge.BuildSnapshot(nodes, bridge.FilterInteractive, -1)
	_ = bridge.EnrichA11yNodesWithDOMMetadata(ctx, flat)
	h.Bridge.SetRefCache(tabID, &bridge.RefCache{
		Refs:    refs,
		Targets: bridge.RefTargetsFromNodes(flat),
		Nodes:   flat,
	})
}

func isTimeoutWithPendingDialog(err error, tabID string, b bridge.BridgeAPI) bool {
	if err == nil {
		return false
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return pendingTabDialog(b, tabID) != nil
}

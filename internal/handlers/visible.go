package handlers

import (
	"context"
	"net/http"
)

type visibleResponse struct {
	Ref     string `json:"ref"`
	Visible bool   `json:"visible"`
}

// HandleGetVisible returns whether an element identified by a unified selector
// (ref/css/xpath/text/semantic) is visible on the page.
//
// @Endpoint GET /visible
func (h *Handlers) HandleGetVisible(w http.ResponseWriter, r *http.Request) {
	h.serveElementInspection(w, r, "inspect.visible", func(ctx context.Context, tabID, sel string) (any, error) {
		visible, err := h.getElementVisible(ctx, tabID, sel)
		if err != nil {
			return nil, err
		}
		return visibleResponse{Ref: sel, Visible: visible}, nil
	})
}

// HandleTabGetVisible returns visibility for a tab identified by path ID.
//
// @Endpoint GET /tabs/{id}/visible
func (h *Handlers) HandleTabGetVisible(w http.ResponseWriter, r *http.Request) {
	h.withPathTabID(w, r, h.HandleGetVisible)
}

// elementVisibleJS asks whether CSS renders an element. The position comes from the
// computed style, not el.style: el.style is the inline attribute alone, and offsetParent is
// null for every fixed element by design — so the branch that exists to rescue that case
// only fired when the author happened to write position in a style= attribute, and a modal,
// banner or toolbar positioned by a class or stylesheet rule was reported not visible while
// sitting on screen with real geometry.
const elementVisibleJS = `function() {
  var el = this;
  var style = window.getComputedStyle(el);
  if (!el.offsetParent && style.position !== 'fixed' && style.position !== 'sticky') return false;
  if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') return false;
  var rect = el.getBoundingClientRect();
  return rect.width > 0 && rect.height > 0;
}`

// getElementVisible resolves a unified selector to a DOM node and checks whether it is visible.
func (h *Handlers) getElementVisible(ctx context.Context, tabID, sel string) (bool, error) {
	return callOnResolvedElement[bool](h, ctx, tabID, sel, elementVisibleJS, nil)
}

package handlers

import (
	"net/http"

	"github.com/pinchtab/pinchtab/internal/browserprobe"
	"github.com/pinchtab/pinchtab/internal/browsers/runtimekit"
	"github.com/pinchtab/pinchtab/internal/config"
	"github.com/pinchtab/pinchtab/internal/httpx"
)

// browserVersionCache keeps the per-binary probe off the request path. The
// cache seam that tests exercise lives in browserprobe.VersionCache; this is
// only the process-wide instance the handler reads through.
var browserVersionCache = browserprobe.NewVersionCache(browserprobe.RunVersion)

func (h *Handlers) HandleStealthStatus(w http.ResponseWriter, r *http.Request) {
	status := h.Bridge.StealthStatus()
	if status == nil {
		writeUnavailable(w, 503, "stealth_unavailable", "stealth bundle unavailable")
		return
	}
	// Both fields come from one config here so they can never be read from
	// different sources: what the persona claims, and what the binary that
	// config resolves to reports for itself.
	if h.Config != nil {
		status.AdvertisedBrowserVersion = h.Config.BrowserVersion
	}
	status.BrowserBinaryVersion = probedBrowserVersion(r, h.Config)
	if tabID := r.URL.Query().Get("tabId"); tabID != "" {
		if tracker, ok := h.Bridge.(interface{ FingerprintRotateActive(string) bool }); ok {
			status.TabOverrides["fingerprintRotateActive"] = tracker.FingerprintRotateActive(tabID)
		}
	}
	httpx.JSON(w, 200, status)
}

// probedBrowserVersion reports the version the launched binary states for
// itself, resolved through the same resolver the launch path uses. It is empty
// when no binary is found or the probe fails, so a caller can tell "unknown"
// from a real disagreement with the advertised version.
func probedBrowserVersion(r *http.Request, cfg *config.RuntimeConfig) string {
	if cfg == nil {
		return ""
	}
	return browserVersionCache.Version(r.Context(), runtimekit.ResolveEffectiveBrowser(cfg).Binary)
}

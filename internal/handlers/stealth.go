package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/pinchtab/pinchtab/internal/activity"
	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/httpx"
	"github.com/pinchtab/pinchtab/internal/stealth"
)

type fingerprintRequest struct {
	TabID    string `json:"tabId"`
	OS       string `json:"os"`
	Browser  string `json:"browser"`
	Screen   string `json:"screen"`
	Language string `json:"language"`
	Timezone int    `json:"timezone"`
	WebGL    bool   `json:"webgl"`
	Canvas   bool   `json:"canvas"`
	Fonts    bool   `json:"fonts"`
	Audio    bool   `json:"audio"`
}

func (h *Handlers) HandleFingerprintRotate(w http.ResponseWriter, r *http.Request) {
	var req fingerprintRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodySize)).Decode(&req); err != nil {
		httpx.Error(w, 400, fmt.Errorf("decode: %w", err))
		return
	}

	ctx, resolvedTabID, err := h.tabContext(r, req.TabID)
	if err != nil {
		WriteTabContextError(w, err, 404)
		return
	}
	if _, ok := h.enforceCurrentTabDomainPolicy(w, r, ctx, resolvedTabID); !ok {
		return
	}

	fp, err := h.generateFingerprint(req)
	if err != nil {
		httpx.ErrorCode(w, 400, "unsupported_fingerprint", err.Error(), false, nil)
		return
	}

	tCtx, tCancel := context.WithTimeout(ctx, 5*time.Second)
	defer tCancel()

	if err := h.Bridge.SetUserAgentOverride(tCtx, bridge.UserAgentOverrideParams{
		UserAgent:      fp.UserAgent,
		Platform:       fp.Platform,
		AcceptLanguage: fp.Language,
	}); err != nil {
		httpx.Error(w, 500, fmt.Errorf("setUserAgentOverride: %w", err))
		return
	}
	if fp.Language != "" {
		if err := h.Bridge.SetLocaleOverride(tCtx, fp.Language); err != nil {
			httpx.Error(w, 500, fmt.Errorf("setLocaleOverride: %w", err))
			return
		}
	}
	if timezoneID := timezoneIDFromOffset(fp.TimezoneOffset); timezoneID != "" {
		if err := h.Bridge.SetTimezoneOverride(tCtx, timezoneID); err != nil {
			httpx.Error(w, 500, fmt.Errorf("setTimezoneOverride: %w", err))
			return
		}
	}
	if fp.ScreenWidth > 0 && fp.ScreenHeight > 0 {
		if err := h.Bridge.SetDeviceMetricsOverride(tCtx, bridge.DeviceMetricsOverrideParams{
			Width:             int64(fp.ScreenWidth),
			Height:            int64(fp.ScreenHeight),
			DeviceScaleFactor: 1,
			Mobile:            false,
			ScreenWidth:       int64(fp.ScreenWidth),
			ScreenHeight:      int64(fp.ScreenHeight),
		}); err != nil {
			httpx.Error(w, 500, fmt.Errorf("setDeviceMetricsOverride: %w", err))
			return
		}
	}
	if fp.Platform != "" {
		overlayScript := fingerprintRotatePlatformOverlayScript(fp.Platform)
		if _, err := h.Bridge.AddScriptToEvaluateOnNewDocument(tCtx, overlayScript); err != nil {
			httpx.Error(w, 500, fmt.Errorf("add platform overlay: %w", err))
			return
		}
		if err := h.Bridge.Evaluate(tCtx, overlayScript, nil, bridge.EvalOpts{}); err != nil {
			httpx.Error(w, 500, fmt.Errorf("evaluate platform overlay: %w", err))
			return
		}
	}

	if tracker, ok := h.Bridge.(interface{ SetFingerprintRotateActive(string, bool) }); ok {
		tracker.SetFingerprintRotateActive(resolvedTabID, true)
	}

	h.recordActivity(r, activity.Update{Action: "fingerprint.rotate", TabID: resolvedTabID})

	httpx.JSON(w, 200, map[string]any{
		"fingerprint": fp,
		"status":      "rotated",
	})
}

type fingerprint struct {
	UserAgent      string `json:"userAgent"`
	Platform       string `json:"platform"`
	Vendor         string `json:"vendor"`
	ScreenWidth    int    `json:"screenWidth"`
	ScreenHeight   int    `json:"screenHeight"`
	Language       string `json:"language"`
	TimezoneOffset int    `json:"timezoneOffset"`
	CPUCores       int    `json:"cpuCores"`
	Memory         int    `json:"memory"`
}

// fingerprintMatrix is the set of identities this endpoint will hand out. The
// matrix is irregular — windows has chrome and edge, mac has chrome and safari,
// linux has chrome only — so the pairs that do not exist are not obvious from
// outside, which is why a miss is refused rather than answered with a blank
// identity. It is a function so the refusal message can be derived from it and a
// test can add a pair without a second list to edit.
func (h *Handlers) fingerprintMatrix() map[string]map[string]fingerprint {
	// Match the launch-pinned UA: real Chrome (UA reduction, v100+) freezes
	// navigator.userAgent to <major>.0.0.0. Using h.Config.BrowserVersion
	// verbatim here would emit Chrome/144.0.7559.133 while the launch path
	// pins Chrome/144.0.0.0 — a page/post-rotate version drift.
	reducedBrowserVersion := stealth.ReducedBrowserVersion(h.Config.BrowserVersion)

	// The Chrome UA strings come from stealth.ChromeUserAgent, the same template the
	// launch persona uses, so the frozen OS tokens are spelled out once. mac/safari
	// stays a literal on purpose: it is a Safari UA, not the Chrome template with a
	// different OS token, and folding it in would manufacture a false sibling.
	windowsChrome := stealth.ChromeUserAgent(stealth.PlatformWindows, reducedBrowserVersion)
	macChrome := stealth.ChromeUserAgent(stealth.PlatformMacOS, reducedBrowserVersion)
	linuxChrome := stealth.ChromeUserAgent(stealth.PlatformLinux, reducedBrowserVersion)

	osConfigs := map[string]map[string]fingerprint{
		"windows": {
			"chrome": {
				UserAgent: windowsChrome,
				Platform:  "Win32",
				Vendor:    "Google Inc.",
			},
			"edge": {
				UserAgent: stealth.EdgeUserAgent(windowsChrome, reducedBrowserVersion),
				Platform:  "Win32",
				Vendor:    "Google Inc.",
			},
		},
		"mac": {
			"chrome": {
				UserAgent: macChrome,
				Platform:  "MacIntel",
				Vendor:    "Google Inc.",
			},
			"safari": {
				UserAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15",
				Platform:  "MacIntel",
				Vendor:    "Apple Computer, Inc.",
			},
		},
		// A Linux host asking for a fingerprint used to get a foreign one, since only
		// windows and mac had entries. os: "random" stays windows-or-mac: adding linux
		// to the weighted pick would change what a default request returns.
		"linux": {
			"chrome": {
				UserAgent: linuxChrome,
				Platform:  "Linux x86_64",
				Vendor:    "Google Inc.",
			},
		},
	}

	return osConfigs
}

// availableFingerprintPairs lists the matrix as os/browser strings, sorted so the
// refusal message reads the same every run.
func availableFingerprintPairs(matrix map[string]map[string]fingerprint) []string {
	pairs := make([]string, 0, len(matrix)*2)
	for osName, browsers := range matrix {
		for browserName := range browsers {
			pairs = append(pairs, osName+"/"+browserName)
		}
	}
	sort.Strings(pairs)
	return pairs
}

// randomFingerprintOSWeights is the weighted pick behind os: "random". linux is
// absent on purpose: adding it would change what a default request returns.
var randomFingerprintOSWeights = []struct {
	name   string
	weight float64
}{
	{name: "windows", weight: 0.7},
	{name: "mac", weight: 0.3},
}

// resolveRandomFingerprintOS picks among the weighted os rows that actually hold
// the requested browser, re-weighting over those candidates. Picking an os first
// and looking the pair up second made os: "random" answer 200 or 400 by coin flip
// for browsers only one os holds — safari refused whenever the pick was windows.
// It reports no candidate when no weighted row holds the browser at all, which is
// still a refusal: the constraint narrows the pick, it does not remove the refusal.
func resolveRandomFingerprintOS(matrix map[string]map[string]fingerprint, browser string) (string, bool) {
	total := 0.0
	for _, candidate := range randomFingerprintOSWeights {
		if _, ok := matrix[candidate.name][browser]; ok {
			total += candidate.weight
		}
	}
	if total == 0 {
		return "", false
	}

	draw := rand.Float64() * total
	picked := ""
	for _, candidate := range randomFingerprintOSWeights {
		if _, ok := matrix[candidate.name][browser]; !ok {
			continue
		}
		picked = candidate.name
		draw -= candidate.weight
		if draw < 0 {
			break
		}
	}
	return picked, true
}

// generateFingerprint refuses an os/browser pair the matrix does not hold instead
// of returning the zero identity. An empty userAgent delivered as a success is
// worse than a refusal: the endpoint exists to hand back an identity to apply, and
// the caller cannot tell it received nothing. Falling back to the os's chrome entry
// would be the same defect one layer along — a plausible identity that is not the
// one requested is harder to notice than a blank one.
func (h *Handlers) generateFingerprint(req fingerprintRequest) (fingerprint, error) {
	fp := fingerprint{}
	osConfigs := h.fingerprintMatrix()

	browser := req.Browser
	if browser == "" {
		browser = "chrome"
	}

	// The random pick is constrained by the browser, so the message can only ever
	// name an os the caller supplied. A refusal reporting "windows" to someone who
	// asked for os: "random" is not actionable — retrying may well succeed.
	os := req.OS
	if os == "random" {
		picked, ok := resolveRandomFingerprintOS(osConfigs, browser)
		if !ok {
			return fingerprint{}, fmt.Errorf("no fingerprint for browser %q under os %q; available pairs: %s",
				browser, req.OS, strings.Join(availableFingerprintPairs(osConfigs), ", "))
		}
		os = picked
	}

	// Before anything else is set, so a refused request carries no partial identity:
	// a randomised screen size and core count alongside a 400 would imply the
	// request was honoured.
	browserConfig, ok := osConfigs[os][browser]
	if !ok {
		return fingerprint{}, fmt.Errorf("no fingerprint for os %q with browser %q; available pairs: %s",
			os, browser, strings.Join(availableFingerprintPairs(osConfigs), ", "))
	}
	fp.UserAgent = browserConfig.UserAgent
	fp.Platform = browserConfig.Platform
	fp.Vendor = browserConfig.Vendor

	screens := [][]int{
		{1920, 1080}, {1366, 768}, {1536, 864}, {1440, 900},
		{1280, 720}, {1600, 900}, {2560, 1440},
	}
	if req.Screen == "random" {
		screen := screens[rand.Intn(len(screens))]
		fp.ScreenWidth = screen[0]
		fp.ScreenHeight = screen[1]
	} else if req.Screen != "" {
		_, _ = fmt.Sscanf(req.Screen, "%dx%d", &fp.ScreenWidth, &fp.ScreenHeight)
	} else {
		fp.ScreenWidth = 1920
		fp.ScreenHeight = 1080
	}

	if req.Language != "" {
		fp.Language = req.Language
	} else {
		fp.Language = "en-US"
	}

	if req.Timezone != 0 {
		fp.TimezoneOffset = req.Timezone
	} else {
		fp.TimezoneOffset = -300
	}

	fp.CPUCores = 4 + rand.Intn(4)*2
	fp.Memory = 4 + rand.Intn(4)*2

	return fp, nil
}

func timezoneIDFromOffset(offset int) string {
	switch offset {
	case -480:
		return "America/Los_Angeles"
	case -420:
		return "America/Denver"
	case -360:
		return "America/Chicago"
	case -300:
		return "America/New_York"
	case -240:
		return "America/Halifax"
	case 0:
		return "UTC"
	case 60:
		return "Europe/Berlin"
	case 120:
		return "Europe/Helsinki"
	case 330:
		return "Asia/Kolkata"
	case 480:
		return "Asia/Shanghai"
	case 540:
		return "Asia/Tokyo"
	default:
		return ""
	}
}

func fingerprintRotatePlatformOverlayScript(platform string) string {
	return fmt.Sprintf(`(() => {
  try {
    const nav = navigator;
    const proto = Object.getPrototypeOf(nav) || Navigator.prototype;
    Object.defineProperty(proto, 'platform', {
      get: () => %q,
      configurable: true,
      enumerable: true
    });
  } catch (e) {}
})()`, platform)
}

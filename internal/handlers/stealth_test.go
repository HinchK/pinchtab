package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/assets"
	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/config"
	"github.com/pinchtab/pinchtab/internal/stealth"
)

func TestHandleFingerprintRotate_InvalidJSON(t *testing.T) {
	h := New(&mockBridge{}, &config.RuntimeConfig{}, nil, nil, nil)
	req := httptest.NewRequest("POST", "/fingerprint/rotate", bytes.NewReader([]byte(`not json`)))
	w := httptest.NewRecorder()

	h.HandleFingerprintRotate(w, req)

	if w.Code != 400 {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestGenerateFingerprint_Windows(t *testing.T) {
	h := Handlers{Config: &config.RuntimeConfig{BrowserVersion: "120.0.0.0"}}
	fp, err := h.generateFingerprint(fingerprintRequest{OS: "windows"})
	if err != nil {
		t.Fatalf("generateFingerprint: %v", err)
	}
	if fp.Platform != "Win32" {
		t.Errorf("expected Win32, got %q", fp.Platform)
	}
	if fp.UserAgent == "" {
		t.Error("expected non-empty user agent")
	}
	if fp.ScreenWidth == 0 || fp.ScreenHeight == 0 {
		t.Error("expected non-zero screen dimensions")
	}
	if fp.Vendor != "Google Inc." {
		t.Errorf("expected Google Inc., got %q", fp.Vendor)
	}
}

func TestGenerateFingerprint_Mac(t *testing.T) {
	h := Handlers{Config: &config.RuntimeConfig{BrowserVersion: "120.0.0.0"}}
	fp, err := h.generateFingerprint(fingerprintRequest{OS: "mac"})
	if err != nil {
		t.Fatalf("generateFingerprint: %v", err)
	}
	if fp.Platform != "MacIntel" {
		t.Errorf("expected MacIntel, got %q", fp.Platform)
	}
}

func TestGenerateFingerprint_Random(t *testing.T) {
	h := Handlers{Config: &config.RuntimeConfig{BrowserVersion: "120.0.0.0"}}
	fp, err := h.generateFingerprint(fingerprintRequest{OS: "random"})
	if err != nil {
		t.Fatalf("generateFingerprint: %v", err)
	}
	validPlatforms := map[string]bool{"Win32": true, "MacIntel": true}
	if !validPlatforms[fp.Platform] {
		t.Errorf("unexpected platform %q", fp.Platform)
	}
}

func TestGenerateFingerprint_WithBrowser(t *testing.T) {
	h := Handlers{Config: &config.RuntimeConfig{BrowserVersion: "120.0.0.0"}}
	fp, err := h.generateFingerprint(fingerprintRequest{OS: "windows", Browser: "chrome"})
	if err != nil {
		t.Fatalf("generateFingerprint: %v", err)
	}
	if fp.UserAgent == "" {
		t.Error("expected non-empty user agent")
	}
}

func TestGenerateFingerprint_Config(t *testing.T) {
	cfg := &config.RuntimeConfig{BrowserVersion: "120.0.0.0"}
	h := Handlers{Config: cfg}

	fp, err := h.generateFingerprint(fingerprintRequest{OS: "windows", Browser: "chrome"})
	if err != nil {
		t.Fatalf("generateFingerprint: %v", err)
	}
	if !strings.Contains(fp.UserAgent, "120.0.0.0") {
		t.Errorf("expected User-Agent to contain Chrome version 120.0.0.0, got %q", fp.UserAgent)
	}
}

// The launch path pins navigator.userAgent to Chrome's UA-reduced form
// (Chrome/<major>.0.0.0). /fingerprint/rotate runs against the same browser
// session and MUST emit the same reduced form, otherwise an initial UA of
// Chrome/144.0.0.0 followed by a rotated UA of Chrome/144.0.7559.133 trips
// the "Chrome version preserved" E2E contract (system-basic.sh).
func TestGenerateFingerprint_MatchesLaunchPinnedUAReduction(t *testing.T) {
	cfg := &config.RuntimeConfig{BrowserVersion: "144.0.7559.133", Headless: true}
	bundle := stealth.NewBundle(cfg, 1)
	launchUA := bundle.LaunchUserAgent()
	if launchUA == "" {
		t.Fatalf("precondition: headless launch must pin a UA, got empty")
	}
	if !strings.Contains(launchUA, "Chrome/144.0.0.0") {
		t.Fatalf("precondition: launch UA should be reduced to Chrome/144.0.0.0, got %q", launchUA)
	}

	h := Handlers{Config: cfg}
	for _, tc := range []struct {
		os, browser string
	}{
		{"windows", "chrome"},
		{"mac", "chrome"},
		{"windows", "edge"},
	} {
		fp, err := h.generateFingerprint(fingerprintRequest{OS: tc.os, Browser: tc.browser})
		if err != nil {
			t.Fatalf("generateFingerprint: %v", err)
		}
		if !strings.Contains(fp.UserAgent, "144.0.0.0") {
			t.Errorf("rotate UA for %s/%s should carry reduced Chrome/144.0.0.0, got %q", tc.os, tc.browser, fp.UserAgent)
		}
		if strings.Contains(fp.UserAgent, "144.0.7559.133") {
			t.Errorf("rotate UA for %s/%s leaks full build version (UA reduction violated): %q", tc.os, tc.browser, fp.UserAgent)
		}
	}
}

func TestTimezoneIDFromOffset(t *testing.T) {
	if got := timezoneIDFromOffset(-300); got != "America/New_York" {
		t.Fatalf("timezoneIDFromOffset(-300) = %q, want America/New_York", got)
	}
	if got := timezoneIDFromOffset(999); got != "" {
		t.Fatalf("timezoneIDFromOffset(999) = %q, want empty string", got)
	}
}

func TestFingerprintRotatePlatformOverlayScript(t *testing.T) {
	script := fingerprintRotatePlatformOverlayScript("Win32")
	if !strings.Contains(script, "Object.defineProperty(proto, 'platform'") {
		t.Fatalf("expected platform overlay script to patch navigator platform, got %q", script)
	}
	if !strings.Contains(script, "\"Win32\"") {
		t.Fatalf("expected platform overlay script to embed platform, got %q", script)
	}
}

func TestStealthScript_Content(t *testing.T) {
	if assets.StealthScript == "" {
		t.Fatal("StealthScript is empty")
	}
	if !strings.Contains(assets.StealthScript, "navigator") || !strings.Contains(assets.StealthScript, "webdriver") {
		t.Error("stealth script missing webdriver protection")
	}
	if strings.Contains(assets.StealthScript, "proxyNavigator") || strings.Contains(assets.StealthScript, "Object.defineProperty(window, 'navigator'") {
		t.Error("stealth script should not proxy window.navigator in light mode")
	}
	if !strings.Contains(assets.StealthScript, "downlinkMax") {
		t.Error("stealth script missing downlinkMax coverage")
	}
}

func TestStealthScript_Populated(t *testing.T) {
	b := bridge.New(context.Background(), context.Background(), &config.RuntimeConfig{})

	if b.StealthBundle == nil || b.StealthBundle.Script == "" {
		t.Error("expected stealth bundle script to be populated")
	}
}

func (m *mockBridge) StealthStatus() *stealth.Status {
	return &stealth.Status{
		Level:         stealth.LevelMedium,
		Headless:      true,
		LaunchMode:    stealth.LaunchModeAllocator,
		ScriptHash:    "sha256:test",
		WebdriverMode: stealth.WebdriverModeNativeBaseline,
		Flags: map[string]bool{
			"headlessNew": true,
		},
		Capabilities: map[string]bool{
			"userAgentData":           true,
			"webdriverNativeStrategy": true,
			"downlinkMax":             true,
		},
		TabOverrides: map[string]bool{
			"fingerprintRotateActive": false,
		},
	}
}

func TestHandleStealthStatus(t *testing.T) {
	mb := &mockBridge{fingerprintTabs: map[string]bool{"tab1": true}}
	h := New(mb, &config.RuntimeConfig{}, nil, nil, nil)
	req := httptest.NewRequest("GET", "/stealth/status", nil)
	w := httptest.NewRecorder()

	h.HandleStealthStatus(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if got := resp["level"]; got != "medium" {
		t.Fatalf("expected level=medium, got %v", got)
	}
	if got := resp["launchMode"]; got != "allocator" {
		t.Fatalf("expected launchMode=allocator, got %v", got)
	}
}

func TestHandleStealthStatus_WithTabOverride(t *testing.T) {
	mb := &mockBridge{fingerprintTabs: map[string]bool{"tab-special": true}}
	h := New(mb, &config.RuntimeConfig{}, nil, nil, nil)
	req := httptest.NewRequest("GET", "/stealth/status?tabId=tab-special", nil)
	w := httptest.NewRecorder()

	h.HandleStealthStatus(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	tabOverrides, ok := resp["tabOverrides"].(map[string]any)
	if !ok {
		t.Fatalf("expected tabOverrides object, got %T", resp["tabOverrides"])
	}
	if got := tabOverrides["fingerprintRotateActive"]; got != true {
		t.Fatalf("expected fingerprintRotateActive=true, got %v", got)
	}
}

// The drift this pins already happened once on the version axis and had to be
// resynced by hand; the OS-token axis was the next one. Comparing against
// stealth.ChromeUserAgent rather than against a literal is what makes it
// impossible rather than merely corrected: there is one template, and if it moves,
// both sides move together or this test fails.
func TestGenerateFingerprintUsesTheSharedChromeTemplate(t *testing.T) {
	const version = "144.0.7559.133"
	h := Handlers{Config: &config.RuntimeConfig{BrowserVersion: version}}
	reduced := stealth.ReducedBrowserVersion(version)

	for _, tc := range []struct {
		os, browser  string
		wantPlatform string
		wantUA       string
	}{
		{os: "windows", browser: "chrome", wantPlatform: "Win32", wantUA: stealth.ChromeUserAgent(stealth.PlatformWindows, reduced)},
		{os: "mac", browser: "chrome", wantPlatform: "MacIntel", wantUA: stealth.ChromeUserAgent(stealth.PlatformMacOS, reduced)},
		{os: "linux", browser: "chrome", wantPlatform: "Linux x86_64", wantUA: stealth.ChromeUserAgent(stealth.PlatformLinux, reduced)},
		{os: "windows", browser: "edge", wantPlatform: "Win32", wantUA: stealth.EdgeUserAgent(stealth.ChromeUserAgent(stealth.PlatformWindows, reduced), reduced)},
	} {
		t.Run(tc.os+"/"+tc.browser, func(t *testing.T) {
			fp, err := h.generateFingerprint(fingerprintRequest{OS: tc.os, Browser: tc.browser})
			if err != nil {
				t.Fatalf("generateFingerprint: %v", err)
			}

			if fp.UserAgent != tc.wantUA {
				t.Errorf("user agent =\n %q\nwant the shared template's\n %q", fp.UserAgent, tc.wantUA)
			}
			if fp.Platform != tc.wantPlatform {
				t.Errorf("platform = %q, want %q", fp.Platform, tc.wantPlatform)
			}
			// The reduced version is what real Chrome exposes; a full build here is the
			// exact drift the comment above generateFingerprint was written for.
			if strings.Contains(fp.UserAgent, version) {
				t.Errorf("user agent carries the full build %q: %s", version, fp.UserAgent)
			}
		})
	}
}

// The same template serves the launch persona, so the endpoint and the browser
// PinchTab actually launches cannot describe different Chromes on the same host.
func TestGenerateFingerprintAgreesWithTheLaunchPersonaOnThisHost(t *testing.T) {
	const version = "144.0.7559.133"
	h := Handlers{Config: &config.RuntimeConfig{BrowserVersion: version}}

	hostOS := map[string]string{
		stealth.PlatformWindows: "windows",
		stealth.PlatformMacOS:   "mac",
		stealth.PlatformLinux:   "linux",
	}[stealth.HostPlatform()]

	fp, err := h.generateFingerprint(fingerprintRequest{OS: hostOS, Browser: "chrome"})
	if err != nil {
		t.Fatalf("generateFingerprint: %v", err)
	}
	persona := stealth.BuildPersona("", version)

	if fp.UserAgent != persona.UserAgent {
		t.Fatalf("fingerprint endpoint and launch persona disagree on this host:\n endpoint %q\n persona  %q", fp.UserAgent, persona.UserAgent)
	}
	if fp.Platform != persona.NavigatorPlatform {
		t.Errorf("platform = %q, want the persona's %q", fp.Platform, persona.NavigatorPlatform)
	}
}

// os: "linux" is answerable now; os: "random" is deliberately NOT extended to it,
// because that would change what a default request returns. This states which.
func TestGenerateFingerprintRandomStaysWindowsOrMac(t *testing.T) {
	h := Handlers{Config: &config.RuntimeConfig{BrowserVersion: "144.0.7559.133"}}
	linuxUA := stealth.ChromeUserAgent(stealth.PlatformLinux, stealth.ReducedBrowserVersion("144.0.7559.133"))

	for i := 0; i < 200; i++ {
		fp, err := h.generateFingerprint(fingerprintRequest{OS: "random"})
		if err != nil {
			t.Fatalf("generateFingerprint: %v", err)
		}
		if fp.UserAgent == linuxUA {
			t.Fatalf("os: random returned the Linux UA; adding linux to the weighted pick changes what a default request returns")
		}
		if fp.Platform != "Win32" && fp.Platform != "MacIntel" {
			t.Fatalf("os: random returned platform %q, want Win32 or MacIntel", fp.Platform)
		}
	}
}

// An unlisted pair used to answer 200 with an empty userAgent. For an endpoint
// whose purpose is to hand back an identity to apply, an empty identity delivered
// as a success is worse than a refusal: the caller cannot tell it received nothing.
// linux+edge and mac+edge are real combinations, which is what makes the silence
// reachable by a reasonable request.
func TestGenerateFingerprintRefusesAnUnlistedPair(t *testing.T) {
	h := Handlers{Config: &config.RuntimeConfig{BrowserVersion: "144.0.7559.133"}}

	for _, tc := range []struct{ os, browser string }{
		{os: "linux", browser: "edge"},
		{os: "mac", browser: "edge"},
		{os: "windows", browser: "safari"},
		{os: "bogus", browser: "chrome"},
		{os: "linux", browser: "safari"},
	} {
		t.Run(tc.os+"/"+tc.browser, func(t *testing.T) {
			fp, err := h.generateFingerprint(fingerprintRequest{OS: tc.os, Browser: tc.browser})
			if err == nil {
				t.Fatalf("pair was accepted and returned userAgent %q", fp.UserAgent)
			}
			if fp.UserAgent != "" || fp.Platform != "" || fp.Vendor != "" {
				t.Errorf("refused request still carries an identity: %+v", fp)
			}
			for _, want := range []string{tc.os, tc.browser} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name the requested %q", err, want)
				}
			}
			// The pairs that do exist, so the caller can correct itself in one step.
			for _, want := range availableFingerprintPairs(h.fingerprintMatrix()) {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q omits the available pair %q", err, want)
				}
			}
		})
	}
}

// A 400 must carry no partial identity: a randomised screen size and core count
// beside a refusal would imply the request was honoured.
func TestARefusedFingerprintPopulatesNoOtherField(t *testing.T) {
	h := Handlers{Config: &config.RuntimeConfig{BrowserVersion: "144.0.7559.133"}}

	fp, err := h.generateFingerprint(fingerprintRequest{OS: "linux", Browser: "edge", Screen: "random", Language: "fr-FR", Timezone: -60})
	if err == nil {
		t.Fatal("precondition: linux/edge must be refused")
	}
	if fp != (fingerprint{}) {
		t.Errorf("refused request returned a populated fingerprint: %+v", fp)
	}
}

// The list of valid pairs is derived from the matrix, not restated beside it: a pair
// added to the map appears in the message with no second list to edit.
func TestTheRefusalMessageIsDerivedFromTheMatrix(t *testing.T) {
	h := Handlers{Config: &config.RuntimeConfig{BrowserVersion: "144.0.7559.133"}}

	matrix := h.fingerprintMatrix()
	if got := availableFingerprintPairs(matrix); len(got) == 0 {
		t.Fatal("the matrix is empty — this test would pass vacuously")
	}

	matrix["linux"]["edge"] = fingerprint{UserAgent: "probe", Platform: "Linux x86_64", Vendor: "Google Inc."}
	pairs := availableFingerprintPairs(matrix)

	var found bool
	for _, pair := range pairs {
		if pair == "linux/edge" {
			found = true
		}
	}
	if !found {
		t.Errorf("a pair added to the matrix does not appear in the derived list: %v", pairs)
	}
	if !sort.StringsAreSorted(pairs) {
		t.Errorf("derived pairs are not sorted, so the message varies between runs: %v", pairs)
	}
}

// Every pair that works today must keep working and stay byte-identical, including
// the empty-browser default of chrome.
func TestEveryListedPairStillResolvesByteIdentically(t *testing.T) {
	const version = "144.0.7559.133"
	h := Handlers{Config: &config.RuntimeConfig{BrowserVersion: version}}
	reduced := stealth.ReducedBrowserVersion(version)
	windowsChrome := stealth.ChromeUserAgent(stealth.PlatformWindows, reduced)

	for _, tc := range []struct {
		name         string
		os, browser  string
		wantUA       string
		wantPlatform string
		wantVendor   string
	}{
		{name: "windows/chrome", os: "windows", browser: "chrome", wantUA: windowsChrome, wantPlatform: "Win32", wantVendor: "Google Inc."},
		{name: "windows/edge", os: "windows", browser: "edge", wantUA: stealth.EdgeUserAgent(windowsChrome, reduced), wantPlatform: "Win32", wantVendor: "Google Inc."},
		{name: "mac/chrome", os: "mac", browser: "chrome", wantUA: stealth.ChromeUserAgent(stealth.PlatformMacOS, reduced), wantPlatform: "MacIntel", wantVendor: "Google Inc."},
		{name: "mac/safari", os: "mac", browser: "safari", wantUA: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15", wantPlatform: "MacIntel", wantVendor: "Apple Computer, Inc."},
		{name: "linux/chrome", os: "linux", browser: "chrome", wantUA: stealth.ChromeUserAgent(stealth.PlatformLinux, reduced), wantPlatform: "Linux x86_64", wantVendor: "Google Inc."},
		{name: "empty browser defaults to chrome", os: "linux", browser: "", wantUA: stealth.ChromeUserAgent(stealth.PlatformLinux, reduced), wantPlatform: "Linux x86_64", wantVendor: "Google Inc."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fp, err := h.generateFingerprint(fingerprintRequest{OS: tc.os, Browser: tc.browser})
			if err != nil {
				t.Fatalf("generateFingerprint: %v", err)
			}
			if fp.UserAgent != tc.wantUA {
				t.Errorf("userAgent =\n  %q\nwant\n  %q", fp.UserAgent, tc.wantUA)
			}
			if fp.Platform != tc.wantPlatform {
				t.Errorf("platform = %q, want %q", fp.Platform, tc.wantPlatform)
			}
			if fp.Vendor != tc.wantVendor {
				t.Errorf("vendor = %q, want %q", fp.Vendor, tc.wantVendor)
			}
		})
	}
}

// Scoped to the default browser deliberately — chrome is the one browser both
// weighted os rows hold, so this arm cannot fail whatever the selection does. The
// two tests below carry the non-chrome cases. Run enough times to see both arms of
// the weighting.
func TestRandomOSNeverReachesTheRefusalPath(t *testing.T) {
	h := Handlers{Config: &config.RuntimeConfig{BrowserVersion: "144.0.7559.133"}}

	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		fp, err := h.generateFingerprint(fingerprintRequest{OS: "random"})
		if err != nil {
			t.Fatalf("os=random was refused: %v", err)
		}
		if fp.UserAgent == "" {
			t.Fatal("os=random produced an empty userAgent")
		}
		seen[fp.Platform] = true
	}
	for _, want := range []string{"Win32", "MacIntel"} {
		if !seen[want] {
			t.Errorf("os=random never resolved to %s in 200 draws; the weighting or the matrix changed", want)
		}
	}
}

// os: "random" resolves only to an os whose row holds the requested browser. Picking
// an os first and looking the pair up second made the same request body answer 200 or
// 400 by coin flip: safari refused whenever the pick was windows. Asserted on every
// iteration, because the defect it replaces was intermittent rather than absent.
func TestRandomOSResolvesToAnOSThatHoldsTheRequestedBrowser(t *testing.T) {
	const version = "144.0.7559.133"
	h := Handlers{Config: &config.RuntimeConfig{BrowserVersion: version}}
	reduced := stealth.ReducedBrowserVersion(version)
	windowsChrome := stealth.ChromeUserAgent(stealth.PlatformWindows, reduced)
	matrix := h.fingerprintMatrix()

	for _, tc := range []struct {
		browser      string
		wantUA       string
		wantPlatform string
	}{
		{browser: "safari", wantUA: matrix["mac"]["safari"].UserAgent, wantPlatform: "MacIntel"},
		{browser: "edge", wantUA: stealth.EdgeUserAgent(windowsChrome, reduced), wantPlatform: "Win32"},
	} {
		t.Run(tc.browser, func(t *testing.T) {
			for i := 0; i < 200; i++ {
				fp, err := h.generateFingerprint(fingerprintRequest{OS: "random", Browser: tc.browser})
				if err != nil {
					t.Fatalf("draw %d: os=random browser=%s was refused: %v", i, tc.browser, err)
				}
				if fp.UserAgent != tc.wantUA {
					t.Fatalf("draw %d: userAgent =\n  %q\nwant\n  %q", i, fp.UserAgent, tc.wantUA)
				}
				if fp.Platform != tc.wantPlatform {
					t.Fatalf("draw %d: platform = %q, want %q", i, fp.Platform, tc.wantPlatform)
				}
			}
		})
	}
}

// Narrowing the random pick to the rows that hold the browser must not remove the
// refusal this card exists to add: a browser no weighted row holds is still refused,
// and the message names what the caller SENT. Being told windows/firefox is
// unavailable after asking for os: "random" is not actionable — they never asked for
// windows, and retrying may well succeed.
func TestRandomOSStillRefusesABrowserNoRowHolds(t *testing.T) {
	h := Handlers{Config: &config.RuntimeConfig{BrowserVersion: "144.0.7559.133"}}

	for i := 0; i < 200; i++ {
		fp, err := h.generateFingerprint(fingerprintRequest{OS: "random", Browser: "firefox"})
		if err == nil {
			t.Fatalf("draw %d: os=random browser=firefox was accepted with userAgent %q", i, fp.UserAgent)
		}
		if fp != (fingerprint{}) {
			t.Fatalf("draw %d: refused request returned a populated fingerprint: %+v", i, fp)
		}
		for _, want := range []string{`"firefox"`, `"random"`} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error %q does not name the requested %s", err, want)
			}
		}
		for _, resolved := range []string{`os "windows"`, `os "mac"`} {
			if strings.Contains(err.Error(), resolved) {
				t.Fatalf("error %q reports a resolved os the caller never supplied", err)
			}
		}
		for _, want := range availableFingerprintPairs(h.fingerprintMatrix()) {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error %q omits the available pair %q", err, want)
			}
		}
	}
}

// The weighting over the constrained candidates must stay the weighting: chrome is
// held by both rows, so it keeps the 0.7/0.3 split rather than collapsing to one os.
func TestRandomOSKeepsBothArmsForABrowserBothRowsHold(t *testing.T) {
	h := Handlers{Config: &config.RuntimeConfig{BrowserVersion: "144.0.7559.133"}}

	seen := map[string]int{}
	for i := 0; i < 400; i++ {
		fp, err := h.generateFingerprint(fingerprintRequest{OS: "random", Browser: "chrome"})
		if err != nil {
			t.Fatalf("os=random browser=chrome was refused: %v", err)
		}
		seen[fp.Platform]++
	}
	for _, want := range []string{"Win32", "MacIntel"} {
		if seen[want] == 0 {
			t.Fatalf("os=random browser=chrome never resolved to %s in 400 draws: %v", want, seen)
		}
	}
	if seen["Win32"] <= seen["MacIntel"] {
		t.Errorf("windows is weighted 0.7 against mac 0.3 but drew %v", seen)
	}
}

// The endpoint contract, not just the helper: an unlisted pair must be a 400 with a
// machine-readable code, and must never be a 200 carrying an empty userAgent. The
// refusal also has to happen before the browser is touched — a rejected request must
// not leave a half-applied identity on the tab.
func TestHandleFingerprintRotateRefusesAnUnlistedPair(t *testing.T) {
	for _, body := range []string{
		`{"os":"linux","browser":"edge"}`,
		`{"os":"mac","browser":"edge"}`,
		`{"os":"windows","browser":"safari"}`,
		`{"os":"bogus"}`,
		`{"os":"random","browser":"firefox"}`,
	} {
		t.Run(body, func(t *testing.T) {
			mb := &mockBridge{}
			h := New(mb, &config.RuntimeConfig{BrowserVersion: "144.0.7559.133"}, nil, nil, nil)
			req := httptest.NewRequest("POST", "/fingerprint/rotate", bytes.NewReader([]byte(body)))
			w := httptest.NewRecorder()

			h.HandleFingerprintRotate(w, req)

			if w.Code != 400 {
				t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
			}
			var resp map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("parse body: %v", err)
			}
			if resp["code"] != "unsupported_fingerprint" {
				t.Errorf("code = %v, want unsupported_fingerprint", resp["code"])
			}
			message, _ := resp["error"].(string)
			for _, want := range availableFingerprintPairs(h.fingerprintMatrix()) {
				if !strings.Contains(message, want) {
					t.Errorf("error %q omits the available pair %q", message, want)
				}
			}
			if _, ok := resp["fingerprint"]; ok {
				t.Errorf("a refusal returned a fingerprint payload: %s", w.Body.String())
			}
		})
	}
}

// A listed pair still rotates, so the refusal did not shadow the success path.
func TestHandleFingerprintRotateStillAcceptsAListedPair(t *testing.T) {
	mb := &mockBridge{}
	h := New(mb, &config.RuntimeConfig{BrowserVersion: "144.0.7559.133"}, nil, nil, nil)
	req := httptest.NewRequest("POST", "/fingerprint/rotate", bytes.NewReader([]byte(`{"os":"linux","browser":"chrome"}`)))
	w := httptest.NewRecorder()

	h.HandleFingerprintRotate(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	fp, ok := resp["fingerprint"].(map[string]any)
	if !ok {
		t.Fatalf("no fingerprint in response: %s", w.Body.String())
	}
	if ua, _ := fp["userAgent"].(string); ua == "" {
		t.Error("a rotated fingerprint carries an empty userAgent")
	}
}

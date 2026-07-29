package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
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
	fp := h.generateFingerprint(fingerprintRequest{OS: "windows"})
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
	fp := h.generateFingerprint(fingerprintRequest{OS: "mac"})
	if fp.Platform != "MacIntel" {
		t.Errorf("expected MacIntel, got %q", fp.Platform)
	}
}

func TestGenerateFingerprint_Random(t *testing.T) {
	h := Handlers{Config: &config.RuntimeConfig{BrowserVersion: "120.0.0.0"}}
	fp := h.generateFingerprint(fingerprintRequest{OS: "random"})
	validPlatforms := map[string]bool{"Win32": true, "MacIntel": true}
	if !validPlatforms[fp.Platform] {
		t.Errorf("unexpected platform %q", fp.Platform)
	}
}

func TestGenerateFingerprint_WithBrowser(t *testing.T) {
	h := Handlers{Config: &config.RuntimeConfig{BrowserVersion: "120.0.0.0"}}
	fp := h.generateFingerprint(fingerprintRequest{OS: "windows", Browser: "chrome"})
	if fp.UserAgent == "" {
		t.Error("expected non-empty user agent")
	}
}

func TestGenerateFingerprint_Config(t *testing.T) {
	cfg := &config.RuntimeConfig{BrowserVersion: "120.0.0.0"}
	h := Handlers{Config: cfg}

	fp := h.generateFingerprint(fingerprintRequest{OS: "windows", Browser: "chrome"})
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
		fp := h.generateFingerprint(fingerprintRequest{OS: tc.os, Browser: tc.browser})
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
			fp := h.generateFingerprint(fingerprintRequest{OS: tc.os, Browser: tc.browser})

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

	fp := h.generateFingerprint(fingerprintRequest{OS: hostOS, Browser: "chrome"})
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
		fp := h.generateFingerprint(fingerprintRequest{OS: "random"})
		if fp.UserAgent == linuxUA {
			t.Fatalf("os: random returned the Linux UA; adding linux to the weighted pick changes what a default request returns")
		}
		if fp.Platform != "Win32" && fp.Platform != "MacIntel" {
			t.Fatalf("os: random returned platform %q, want Win32 or MacIntel", fp.Platform)
		}
	}
}

package main

import (
	"testing"

	_ "github.com/pinchtab/pinchtab/internal/browsers/all"
	"github.com/pinchtab/pinchtab/internal/browsers/runtimekit"
	"github.com/pinchtab/pinchtab/internal/config"
)

func TestValidateBridgeCDPURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "browser websocket", raw: "ws://127.0.0.1:9222/devtools/browser/abc"},
		{name: "http origin", raw: "http://127.0.0.1:9222"},
		{name: "json version", raw: "https://cdp.example/json/version"},
		{name: "page websocket rejected", raw: "ws://127.0.0.1:9222/devtools/page/abc", wantErr: true},
		{name: "websocket without browser path rejected", raw: "ws://127.0.0.1:9222", wantErr: true},
		{name: "missing scheme rejected", raw: "127.0.0.1:9222", wantErr: true},
		{name: "unsupported scheme rejected", raw: "ftp://127.0.0.1:9222", wantErr: true},
		{name: "bad http path rejected", raw: "http://127.0.0.1:9222/devtools/page/abc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateBridgeCDPURL(tt.raw)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestResolveBridgeBrowser(t *testing.T) {
	tests := []struct {
		name        string
		browserFlag string
		configured  []string
		want        string
		wantErr     bool
	}{
		{name: "browser flag sets cloak", browserFlag: "cloak", want: "cloak"},
		{name: "browser flag sets chrome", browserFlag: "chrome", want: "chrome"},
		{name: "no flag returns empty", want: ""},
		{name: "invalid browser returns error", browserFlag: "netscape", wantErr: true},
		{name: "configured browser accepted", browserFlag: "my-custom", configured: []string{"my-custom"}, want: "my-custom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveBridgeBrowser(tt.browserFlag, tt.configured)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestBridgeAttachChildFlagContract(t *testing.T) {
	for _, name := range []string{"cdp-attach", "browser", "remote-browser-name"} {
		if bridgeCmd.Flags().Lookup(name) == nil {
			t.Errorf("bridge command missing child flag %q", name)
		}
	}
	if bridgeCmd.Flags().Lookup("browser-provider") != nil {
		t.Error("bridge command must not register obsolete browser-provider flag")
	}
}

func bridgeTargetsConfig() *config.RuntimeConfig {
	return &config.RuntimeConfig{
		DefaultBrowser: config.BrowserCloak,
		DefaultTarget:  "cloak-primary",
		Targets: config.BrowserTargetsConfig{
			"chrome-alt":    {Provider: config.BrowserChrome, Binary: "/tmp/pinchtab-test-chrome"},
			"cloak-primary": {Provider: config.BrowserCloak, Binary: "/tmp/pinchtab-test-cloak"},
		},
	}
}

func TestApplyBridgeBrowserTargetOverridesDefaultTarget(t *testing.T) {
	cfg := bridgeTargetsConfig()

	applyBridgeBrowserTarget(cfg, config.BrowserChrome)

	if cfg.DefaultTarget != "chrome-alt" {
		t.Fatalf("DefaultTarget = %q, want chrome-alt", cfg.DefaultTarget)
	}
	effective := runtimekit.ResolveEffectiveBrowser(cfg)
	if effective.ID != config.BrowserChrome {
		t.Fatalf("resolved provider = %q, want chrome", effective.ID)
	}
	if effective.Binary != "/tmp/pinchtab-test-chrome" {
		t.Fatalf("resolved binary = %q, want the chrome target binary", effective.Binary)
	}
}

func TestApplyBridgeBrowserTargetClearsUnmatchedDefaultTarget(t *testing.T) {
	cfg := bridgeTargetsConfig()

	applyBridgeBrowserTarget(cfg, config.BrowserGhostChrome)

	if cfg.DefaultTarget != "" {
		t.Fatalf("DefaultTarget = %q, want cleared", cfg.DefaultTarget)
	}
	if got := runtimekit.ResolveEffectiveBrowser(cfg).ID; got != config.BrowserGhostChrome {
		t.Fatalf("resolved provider = %q, want ghost-chrome", got)
	}
}

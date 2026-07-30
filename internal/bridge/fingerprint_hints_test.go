package bridge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/pinchtab/pinchtab/internal/config"
	"github.com/pinchtab/pinchtab/internal/stealth"
	"github.com/pinchtab/pinchtab/internal/testbrowser"
)

// The identity the endpoint hands out is only as good as what the browser puts on the
// wire, and the returned payload was already correct while the headers were not — so this
// reads the request Chrome actually made. Rotating used to set the UA and silence every
// Sec-CH-UA header for the life of the tab, which advertised a Windows Edge that sent none
// of the hints a Windows Edge sends.
func TestRotatedIdentitySendsClientHintsThatAgreeWithTheUserAgent(t *testing.T) {
	chromePath := testbrowser.Path(t)

	var mu sync.Mutex
	var last http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		last = r.Header.Clone()
		mu.Unlock()
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<title>hints</title>"))
	}))
	t.Cleanup(server.Close)

	profile := testbrowser.ProfileDir(t)
	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.UserDataDir(profile),
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
	)...)
	ctx, cancelBrowser := chromedp.NewContext(alloc)
	ctx, cancelTimeout := context.WithTimeout(ctx, 40*time.Second)
	t.Cleanup(func() {
		cancelTimeout()
		cancelBrowser()
		cancelAlloc()
		_ = os.RemoveAll(profile)
	})

	load := func() http.Header {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Navigate(server.URL)); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		return last
	}

	// An un-rotated tab is the baseline the fix must not spend: it sends the hints it sends
	// today, so nothing here can be satisfied by moving where they come from.
	baseline := load()
	for _, header := range []string{"Sec-Ch-Ua", "Sec-Ch-Ua-Mobile", "Sec-Ch-Ua-Platform"} {
		if baseline.Get(header) == "" {
			t.Fatalf("an un-rotated tab sent no %s, so this browser does not emit client hints on %s and the test cannot tell the fix from the fixture", header, server.URL)
		}
	}

	b := &Bridge{Config: &config.RuntimeConfig{BrowserVersion: "144.0.7559.133"}}
	reduced := stealth.ReducedBrowserVersion(b.Config.BrowserVersion)
	windowsChrome := stealth.ChromeUserAgent(stealth.PlatformWindows, reduced)

	for _, tc := range []struct {
		name         string
		userAgent    string
		platform     string
		wantBrand    string
		absentBrand  string
		wantPlatform string
	}{
		{
			name:         "windows/edge",
			userAgent:    stealth.EdgeUserAgent(windowsChrome, reduced),
			platform:     "Win32",
			wantBrand:    stealth.BrandEdge,
			absentBrand:  stealth.BrandChrome,
			wantPlatform: `"Windows"`,
		},
		{
			name:         "mac/chrome",
			userAgent:    stealth.ChromeUserAgent(stealth.PlatformMacOS, reduced),
			platform:     "MacIntel",
			wantBrand:    stealth.BrandChrome,
			absentBrand:  stealth.BrandEdge,
			wantPlatform: `"macOS"`,
		},
	} {
		if err := b.SetUserAgentOverride(ctx, UserAgentOverrideParams{
			UserAgent:      tc.userAgent,
			Platform:       tc.platform,
			AcceptLanguage: "en-US",
		}); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}

		headers := load()
		if got := headers.Get("User-Agent"); got != tc.userAgent {
			t.Errorf("%s: User-Agent = %q, want %q", tc.name, got, tc.userAgent)
		}
		brands := headers.Get("Sec-Ch-Ua")
		if brands == "" {
			t.Errorf("%s: no sec-ch-ua header; their total absence is a bot signal on its own", tc.name)
		}
		if !strings.Contains(brands, tc.wantBrand) {
			t.Errorf("%s: sec-ch-ua = %q, want the %q brand the UA claims", tc.name, brands, tc.wantBrand)
		}
		if strings.Contains(brands, tc.absentBrand) {
			t.Errorf("%s: sec-ch-ua = %q, want no %q brand; a UA and hints naming two browsers is a browser that cannot exist", tc.name, brands, tc.absentBrand)
		}
		if got := headers.Get("Sec-Ch-Ua-Platform"); got != tc.wantPlatform {
			t.Errorf("%s: sec-ch-ua-platform = %q, want %s", tc.name, got, tc.wantPlatform)
		}
		if got := headers.Get("Sec-Ch-Ua-Mobile"); got != "?0" {
			t.Errorf("%s: sec-ch-ua-mobile = %q, want ?0", tc.name, got)
		}
	}
}

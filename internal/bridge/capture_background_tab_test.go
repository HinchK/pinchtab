package bridge

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// TestCaptureScreenshotOfBackgroundTabCompletesQuickly is a real-Chromium
// regression test for the bug fixed by always calling Page.bringToFront in
// captureScreenshotWithoutActivation: a genuinely backgrounded tab's
// compositor never resumes painting from Emulation.setFocusEmulationEnabled
// alone, so Page.captureScreenshot previously hung until the caller's
// deadline. A mocked-CDP test can't catch this — the mock "succeeds" at
// focus emulation and returns from captureScreenshot immediately regardless
// of whether a real tab is actually painting.
func TestCaptureScreenshotOfBackgroundTabCompletesQuickly(t *testing.T) {
	chromePath, err := exec.LookPath("chromium")
	if err != nil {
		t.Skip("chromium not installed")
	}
	profile, err := os.MkdirTemp("", "pinchtab-bg-capture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(profile) })

	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.UserDataDir(profile),
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
	)...)
	defer cancelAlloc()

	browserCtx, cancelBrowser := chromedp.NewContext(alloc)
	defer cancelBrowser()
	if err := chromedp.Run(browserCtx); err != nil {
		t.Fatalf("start browser: %v", err)
	}

	html := `<h1>background tab</h1>`
	dataURL := "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(html))

	// Target.createTarget(background=true) is CDP's own mechanism for
	// opening a tab that never becomes the foreground/active one — the same
	// mechanism this codebase's tab manager uses, and exactly the situation
	// (capture on a tab that was never activated) that hung in production.
	var bgTarget target.ID
	if err := chromedp.Run(browserCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		id, err := target.CreateTarget(dataURL).WithBackground(true).Do(ctx)
		bgTarget = id
		return err
	})); err != nil {
		t.Fatalf("create background target: %v", err)
	}

	bgCtx, cancelBg := chromedp.NewContext(browserCtx, chromedp.WithTargetID(bgTarget))
	defer cancelBg()

	// Tight but fair: production uses a 30s ActionTimeout, and a tab that
	// never resumes painting hangs for the whole window. 10s is generous for
	// a real capture to complete, but a regression (activation skipped)
	// fails this test loudly instead of quietly eating a 30s timeout.
	captureCtx, cancelCapture := context.WithTimeout(bgCtx, 10*time.Second)
	defer cancelCapture()

	if err := chromedp.Run(captureCtx); err != nil {
		t.Fatalf("attach to background target: %v", err)
	}

	buf, err := CaptureScreenshot(captureCtx, ScreenshotOpts{Format: page.CaptureScreenshotFormatPng})
	if err != nil {
		t.Fatalf("capture background tab: %v", err)
	}
	if len(buf) == 0 {
		t.Fatal("capture returned no image bytes")
	}
}

// TestCaptureScreenshotOfBackgroundTabBlocksWhenActivationDisabled documents
// the accepted trade-off of DisableActivation: without Page.bringToFront, a
// genuinely backgrounded tab's compositor never resumes, so the capture
// blocks until the context deadline instead of completing. This is the same
// hang TestCaptureScreenshotOfBackgroundTabCompletesQuickly proves is fixed
// by default — this test guards that the opt-out still reproduces it
// deliberately, rather than the fix accidentally covering both cases.
func TestCaptureScreenshotOfBackgroundTabBlocksWhenActivationDisabled(t *testing.T) {
	chromePath, err := exec.LookPath("chromium")
	if err != nil {
		t.Skip("chromium not installed")
	}
	profile, err := os.MkdirTemp("", "pinchtab-bg-capture-noactivate-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(profile) })

	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.UserDataDir(profile),
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
	)...)
	defer cancelAlloc()

	browserCtx, cancelBrowser := chromedp.NewContext(alloc)
	defer cancelBrowser()
	if err := chromedp.Run(browserCtx); err != nil {
		t.Fatalf("start browser: %v", err)
	}

	html := `<h1>background tab</h1>`
	dataURL := "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(html))

	var bgTarget target.ID
	if err := chromedp.Run(browserCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		id, err := target.CreateTarget(dataURL).WithBackground(true).Do(ctx)
		bgTarget = id
		return err
	})); err != nil {
		t.Fatalf("create background target: %v", err)
	}

	bgCtx, cancelBg := chromedp.NewContext(browserCtx, chromedp.WithTargetID(bgTarget))
	defer cancelBg()

	// Short deadline: we expect this to time out, not succeed.
	captureCtx, cancelCapture := context.WithTimeout(bgCtx, 3*time.Second)
	defer cancelCapture()

	if err := chromedp.Run(captureCtx); err != nil {
		t.Fatalf("attach to background target: %v", err)
	}

	_, err = CaptureScreenshot(captureCtx, ScreenshotOpts{
		Format:            page.CaptureScreenshotFormatPng,
		DisableActivation: true,
	})
	if err == nil {
		t.Fatal("capture with activation disabled succeeded on a background tab, want it to block until the deadline")
	}
}

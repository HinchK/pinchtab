package audit

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pinchtab/pinchtab/pkg/pinchtabaudit"
)

func roundTrip[T any](t *testing.T, name string, in T) {
	t.Helper()
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("%s: marshal: %v", name, err)
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("%s: unmarshal: %v", name, err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("%s: round-trip mismatch\n in: %#v\nout: %#v", name, in, out)
	}
}

var ts = time.Date(2026, 7, 4, 12, 30, 0, 0, time.UTC)

func sampleConsoleLogEntry() ConsoleLogEntry {
	return ConsoleLogEntry{Timestamp: ts, Level: "error", Message: "boom", Source: "https://example.com/app.js"}
}

func sampleNetworkRequest() NetworkRequest {
	return NetworkRequest{
		URL: "https://example.com/api", Method: "GET", Status: 500, StatusText: "Internal Server Error",
		ResourceType: "XHR", MimeType: "application/json", StartTime: ts,
		Duration: 12.5, Size: 2048, Failed: true, Error: "net::ERR_FAILED",
	}
}

func sampleBrokenAsset() BrokenAsset {
	return BrokenAsset{URL: "https://example.com/logo.png", ResourceType: "Image", Status: 404, Error: "not found"}
}

func sampleInteractiveElement() InteractiveElement {
	return InteractiveElement{Ref: "e5", Role: "button", Name: "Submit", Tag: "button", Label: "Submit form", Disabled: true}
}

func sampleTimingMetrics() BrowserTimingMetrics {
	return BrowserTimingMetrics{
		TimeToFirstByte: 80, DOMContentLoaded: 350, Load: 900,
		FirstContentfulPaint: 400, LargestContentfulPaint: 850, CumulativeLayoutShift: 0.05,
	}
}

func sampleVisualDiff() VisualDiffResult {
	return VisualDiffResult{
		BaselinePath: "base.png", CurrentPath: "cur.png", DiffPath: "diff.png",
		DiffPixels: 120, DiffRatio: 0.02, Changed: true,
	}
}

func sampleSecurityFinding() SecurityFinding {
	return SecurityFinding{RuleID: "mixed-content", Severity: "medium", Detail: "http resource on https page", URL: "https://example.com"}
}

func sampleBrowserPageData() BrowserPageData {
	diff := sampleVisualDiff()
	return BrowserPageData{
		ScreenshotPath: "page.png", FullPageScreenshot: true,
		ConsoleLogs:         []ConsoleLogEntry{sampleConsoleLogEntry()},
		NetworkRequests:     []NetworkRequest{sampleNetworkRequest()},
		BrokenAssets:        []BrokenAsset{sampleBrokenAsset()},
		InteractiveElements: []InteractiveElement{sampleInteractiveElement()},
		AccessibilityScore:  87,
		VisualDiff:          &diff,
		TimingMetrics:       sampleTimingMetrics(),
	}
}

func samplePageResult() PageResult {
	return PageResult{
		URL: "https://example.com", Title: "Example", StatusCode: 200,
		Seaportal: map[string]any{"group": "home", "wordCount": float64(1200)},
		Browser:   sampleBrowserPageData(),
	}
}

func sampleAuditInput() AuditInput {
	return AuditInput{URLs: []string{"https://example.com"}, SitemapURL: "https://example.com/sitemap.xml", SeaportalFile: "report.json"}
}

func sampleAuditOptions() AuditOptions {
	return AuditOptions{SampleSize: 5, Screenshot: true, NetworkMonitor: true, VisualDiff: true, Concurrency: 4, OutputDir: "out"}
}

func sampleAuditReport() AuditReport {
	r := NewAuditReport()
	r.GeneratedAt = ts
	r.Input = sampleAuditInput()
	r.Options = sampleAuditOptions()
	r.Pages = []PageResult{samplePageResult()}
	r.SummaryScore = 91
	r.SecurityFindings = []SecurityFinding{sampleSecurityFinding()}
	r.Recommendations = []string{"fix broken logo image"}
	return r
}

func TestJSONRoundTrip(t *testing.T) {
	roundTrip(t, "ConsoleLogEntry", sampleConsoleLogEntry())
	roundTrip(t, "NetworkRequest", sampleNetworkRequest())
	roundTrip(t, "BrokenAsset", sampleBrokenAsset())
	roundTrip(t, "InteractiveElement", sampleInteractiveElement())
	roundTrip(t, "BrowserTimingMetrics", sampleTimingMetrics())
	roundTrip(t, "VisualDiffResult", sampleVisualDiff())
	roundTrip(t, "SecurityFinding", sampleSecurityFinding())
	roundTrip(t, "BrowserPageData", sampleBrowserPageData())
	roundTrip(t, "PageResult", samplePageResult())
	roundTrip(t, "AuditInput", sampleAuditInput())
	roundTrip(t, "AuditOptions", sampleAuditOptions())
	roundTrip(t, "AuditReport", sampleAuditReport())
}

func TestNewAuditReportSchemaVersion(t *testing.T) {
	r := NewAuditReport()
	if r.SchemaVersion != SchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", r.SchemaVersion, SchemaVersion)
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := raw["schemaVersion"]; got != SchemaVersion {
		t.Fatalf("schemaVersion JSON field = %v, want %q", got, SchemaVersion)
	}
}

// TestNewAuditReportSchemaVersion above compares the report to the constant that
// stamped it, so it proves the report is stamped and the JSON field is spelled
// right but cannot notice the constant changing. The literal here is the second
// copy that can, and it is the point of the test.
//
// The audit version happens to be rendered into the report goldens, so a bump
// also reds internal/audit/report — but incidentally, and only because those
// goldens compare content: 1.0 and 9.9 are the same width, so a length-only
// golden would miss it. This assertion does not depend on that.
func TestSchemaVersionIsPinnedSoABumpMustBeAcknowledged(t *testing.T) {
	const pinned = "1.0"

	if SchemaVersion != pinned {
		t.Fatalf("audit SchemaVersion = %q, pinned at %q.\n"+
			"An audit schema bump changes what report consumers receive. If it is intended, update all three:\n"+
			"  1. this literal,\n"+
			"  2. the audit report goldens — run `go test ./internal/audit/report -update` and review the diff,\n"+
			"  3. schemaVersion in tests/e2e/fixtures/audit-site/golden-report.json.\n"+
			"The audit e2e scenarios assert the field EXISTS rather than pinning a value, so there is no EXPECTED_*_SCHEMA to change — unlike scrape.",
			SchemaVersion, pinned)
	}
}

// The audit snapshot path never measures layout (no bounds pass, no on-screen
// test), so the report must make no visibility claim at all. The positive
// assertions are here because an empty payload would satisfy the negative.
func TestReportJSONHasNoVisibleKeyForInteractiveElements(t *testing.T) {
	data, err := json.Marshal(sampleAuditReport())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	report := string(data)

	if strings.Contains(report, "visible") {
		t.Errorf("audit report JSON carries a visible key:\n%s", report)
	}
	for _, want := range []string{`"interactiveElements":[{`, `"ref":"e5"`, `"role":"button"`, `"tag":"button"`, `"label":"Submit form"`, `"disabled":true`} {
		if !strings.Contains(report, want) {
			t.Errorf("audit report JSON lost %s:\n%s", want, report)
		}
	}
}

// The SDK mirrors this struct by hand so the public surface never imports
// internal packages, which means nothing but this guard keeps a field removed
// from one side from surviving on the other.
func TestInteractiveElementMatchesTheSDKMirror(t *testing.T) {
	shape := func(rt reflect.Type) []string {
		var out []string
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			out = append(out, f.Name+" "+f.Type.String()+" "+f.Tag.Get("json"))
		}
		return out
	}

	got := shape(reflect.TypeOf(InteractiveElement{}))
	want := shape(reflect.TypeOf(pinchtabaudit.InteractiveElement{}))
	if len(got) == 0 {
		t.Fatal("InteractiveElement has no fields; the guard would pass vacuously")
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("InteractiveElement diverges from the SDK mirror\n internal: %v\n      sdk: %v", got, want)
	}
}

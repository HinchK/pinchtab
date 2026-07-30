package audit

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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

// The SDK mirrors these structs by hand so the public surface never imports
// internal packages, which means nothing but this guard keeps a field removed from
// one side from surviving on the other.
//
// There are two tiers because there are two comparable axes, not because one list
// grew awkward. A struct whose fields are all primitives can be compared on name +
// TYPE + tag — a changed primitive type is a decode failure, the one divergence that
// actually breaks a consumer. A struct holding mirrored structs cannot: its field
// types read as audit.ConsoleLogEntry on one side and pinchtabaudit.ConsoleLogEntry
// on the other, which is package qualification rather than divergence. Those are
// compared on name + tag, which still catches a field added or removed on one side —
// and they are the top-level payload shapes, so they are the likeliest to gain one.
type mirrorPair struct {
	// internalName and sdkName are the declared type names. They are carried
	// explicitly rather than derived because two pairs are not name-equal, and the
	// census below credits a shared name only when a pair claims it on both sides.
	internalName string
	sdkName      string
	internal     any
	sdk          any
}

// mirrorPairsWithTypes are compared on name + type + tag: every field is a
// primitive, a slice of primitives, or a time, so no field type is package-qualified.
var mirrorPairsWithTypes = []mirrorPair{
	{"InteractiveElement", "InteractiveElement", InteractiveElement{}, pinchtabaudit.InteractiveElement{}},
	{"ConsoleLogEntry", "ConsoleLogEntry", ConsoleLogEntry{}, pinchtabaudit.ConsoleLogEntry{}},
	{"JSError", "JSError", JSError{}, pinchtabaudit.JSError{}},
	{"NetworkRequest", "NetworkRequest", NetworkRequest{}, pinchtabaudit.NetworkRequest{}},
	{"BrokenAsset", "BrokenAsset", BrokenAsset{}, pinchtabaudit.BrokenAsset{}},
	{"SecurityFinding", "SecurityFinding", SecurityFinding{}, pinchtabaudit.SecurityFinding{}},
	{"A11yFinding", "A11yFinding", A11yFinding{}, pinchtabaudit.A11yFinding{}},
	{"VisualDiffResult", "VisualDiffResult", VisualDiffResult{}, pinchtabaudit.VisualDiffResult{}},
	{"AuditOptions", "AuditOptions", AuditOptions{}, pinchtabaudit.AuditOptions{}},
	// Not name-equal: the SDK drops the Browser prefix it has no other timings to
	// distinguish from. A name-equality census would report both as unguarded.
	{"BrowserTimingMetrics", "TimingMetrics", BrowserTimingMetrics{}, pinchtabaudit.TimingMetrics{}},
	// Also not name-equal, and for a sharper reason: the SDK's AuditInput is the
	// REQUEST shape, so the report's input block needed its own mirror.
	{"AuditInput", "AuditReportInput", AuditInput{}, pinchtabaudit.AuditReportInput{}},
}

// mirrorPairsWithoutTypes are compared on name + tag only, because their field types
// are package-qualified mirrors of the structs above.
var mirrorPairsWithoutTypes = []mirrorPair{
	{"BrowserPageData", "BrowserPageData", BrowserPageData{}, pinchtabaudit.BrowserPageData{}},
	{"PageResult", "PageResult", PageResult{}, pinchtabaudit.PageResult{}},
	{"PageAudit", "PageAudit", PageAudit{}, pinchtabaudit.PageAudit{}},
	{"AuditReport", "AuditReport", AuditReport{}, pinchtabaudit.AuditReport{}},
}

// unmirroredSharedNames are type names both packages export that are deliberately NOT
// mirrors, each with its own reason. One blanket rationale is what let a live
// divergence hide here before: it covered a deliberate difference, two accidental
// omissions and an unchosen tag at once.
var unmirroredSharedNames = map[string]string{
	"AuditInput": "the two same-named types describe opposite directions: internal AuditInput is the report's input block, mirrored by the SDK's AuditReportInput and guarded as that pair, while the SDK's AuditInput carries SeaportalResults — raw bytes a caller sends and the server never echoes back",
	"PageOptions": "internal resolves each collector to a bool; the SDK uses *bool so nil means " +
		"keep the server default, which is a deliberately different wire contract rather than a drifted mirror",
	"RunOptions": "an in-process options struct on both sides, never marshalled: internal carries no json tags at all and the SDK nests *PageOptions",
}

func mirrorShape(v any, withTypes bool) []string {
	rt := reflect.TypeOf(v)
	var out []string
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if withTypes {
			out = append(out, f.Name+" "+f.Type.String()+" "+f.Tag.Get("json"))
			continue
		}
		out = append(out, f.Name+" "+f.Tag.Get("json"))
	}
	return out
}

func TestAuditPayloadTypesMatchTheirSDKMirrors(t *testing.T) {
	for _, tier := range []struct {
		name      string
		pairs     []mirrorPair
		withTypes bool
	}{
		{"name+type+tag", mirrorPairsWithTypes, true},
		{"name+tag", mirrorPairsWithoutTypes, false},
	} {
		for _, pair := range tier.pairs {
			t.Run(tier.name+"/"+pair.internalName, func(t *testing.T) {
				got := mirrorShape(pair.internal, tier.withTypes)
				want := mirrorShape(pair.sdk, tier.withTypes)
				if len(got) == 0 {
					t.Fatalf("%s has no fields; the guard would pass vacuously", pair.internalName)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s diverges from its SDK mirror %s\n internal: %v\n      sdk: %v",
						pair.internalName, pair.sdkName, got, want)
				}
			})
		}
	}
}

// The census that closes the class. One hardcoded struct became nine, and nothing
// stopped a tenth mirrored struct from being added to both packages and covered by
// neither. Every type name exported by BOTH packages must be claimed by a pair on
// both sides or carry a reason in unmirroredSharedNames; a name in neither fails.
func TestEverySharedAuditTypeNameIsClaimedOrExcused(t *testing.T) {
	internalNames := exportedStructNames(t, ".")
	sdkNames := exportedStructNames(t, filepath.Join("..", "..", "pkg", "pinchtabaudit"))

	claimedInternal := map[string]bool{}
	claimedSDK := map[string]bool{}
	for _, pair := range append(append([]mirrorPair{}, mirrorPairsWithTypes...), mirrorPairsWithoutTypes...) {
		if !internalNames[pair.internalName] {
			t.Errorf("pair names internal type %q, which internal/audit does not declare", pair.internalName)
		}
		if !sdkNames[pair.sdkName] {
			t.Errorf("pair names SDK type %q, which pkg/pinchtabaudit does not declare", pair.sdkName)
		}
		claimedInternal[pair.internalName] = true
		claimedSDK[pair.sdkName] = true
	}

	var shared int
	for name := range internalNames {
		if !sdkNames[name] {
			continue
		}
		shared++
		if claimedInternal[name] && claimedSDK[name] {
			continue
		}
		if unmirroredSharedNames[name] != "" {
			continue
		}
		t.Errorf("%s is exported by both internal/audit and pkg/pinchtabaudit but is in no comparison tier "+
			"and has no reason in unmirroredSharedNames; add it to one", name)
	}
	if shared == 0 {
		t.Fatal("no shared type names found; this census would pass vacuously")
	}

	for name := range unmirroredSharedNames {
		if !internalNames[name] || !sdkNames[name] {
			t.Errorf("unmirroredSharedNames excuses %q, which is no longer exported by both packages", name)
		}
		// An excuse that also names a guarded pair is the next version of the blanket
		// rationale: it reads as covered from either table, so deleting the pair later
		// looks safe. AuditInput is excused for its NAME while internal AuditInput is
		// paired with the SDK's AuditReportInput, which is why the check is symmetry.
		if claimedInternal[name] && claimedSDK[name] {
			t.Errorf("%q is both excused in unmirroredSharedNames and compared as a pair; keep one, or deleting the pair will read as covered", name)
		}
	}
}

// exportedStructNames scans a package directory for `type X struct` declarations.
// Reflection cannot enumerate a package, and the mirror lives in two of them.
func exportedStructNames(t *testing.T, dir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	names := map[string]bool{}
	fset := token.NewFileSet()
	var scanned int
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		scanned++
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || !ts.Name.IsExported() {
					continue
				}
				if _, ok := ts.Type.(*ast.StructType); ok {
					names[ts.Name.Name] = true
				}
			}
		}
	}
	if scanned == 0 {
		t.Fatalf("no production files scanned in %s; the census would pass vacuously", dir)
	}
	return names
}

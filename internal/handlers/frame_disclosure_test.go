package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/config"
	"github.com/pinchtab/pinchtab/internal/testbrowser"
)

// frameIdentityBridge answers the in-frame identity read, which is how the disclosure learns
// what the content it is describing actually belongs to.
type frameIdentityBridge struct {
	*mockBridge
	askedFrameID string
	title, url   string
	fail         bool
}

func (b *frameIdentityBridge) EvaluateInFrame(_ context.Context, frameID string, _ string, result any, _ bridge.EvalOpts) error {
	b.askedFrameID = frameID
	if b.fail {
		return context.Canceled
	}
	payload, err := json.Marshal(map[string]string{"title": b.title, "url": b.url})
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, result)
}

func newScopedHandlers(t *testing.T, scope bridge.FrameScope, identity *frameIdentityBridge) *Handlers {
	t.Helper()
	mb := &mockBridge{frameScopes: map[string]bridge.FrameScope{"tab1": scope}}
	identity.mockBridge = mb
	return New(identity, &config.RuntimeConfig{}, nil, nil, nil)
}

// The common case must gain nothing: an unscoped read has no frame to disclose, and a payload
// that grew a key for every whole-document read would be a change to every caller.
func TestFrameDisclosureIsAbsentForAWholeDocumentRead(t *testing.T) {
	h := newScopedHandlers(t, bridge.FrameScope{}, &frameIdentityBridge{})

	if got := h.frameDisclosureFor(context.Background(), "tab1", ""); got != nil {
		t.Errorf("frameDisclosureFor = %+v, want nil for an unscoped read", got)
	}
	if got := (*frameDisclosure)(nil).attach(map[string]any{"url": "u"}); len(got) != 1 {
		t.Errorf("attach added %v to an unscoped payload", got)
	}
	if marker := (*frameDisclosure)(nil).marker(); marker != "" {
		t.Errorf("marker = %q, want empty for an unscoped read", marker)
	}
}

// The disclosure has to carry the frame's OWN url and title: the complaint this closes is
// attribution, and an id alone does not tell a reader which document the content came from.
func TestFrameDisclosureCarriesTheFramesOwnIdentity(t *testing.T) {
	identity := &frameIdentityBridge{title: "Inner", url: "http://127.0.0.1:1/inner.html"}
	h := newScopedHandlers(t, bridge.FrameScope{
		FrameID:   "frame-1",
		FrameURL:  "http://127.0.0.1:1/stale.html",
		FrameName: "payment",
		OwnerRef:  "e3",
	}, identity)

	got := h.frameDisclosureFor(context.Background(), "tab1", "frame-1")
	if got == nil {
		t.Fatal("a scoped read disclosed nothing")
	}
	if identity.askedFrameID != "frame-1" {
		t.Errorf("the identity was read in frame %q, want the frame the read was served from", identity.askedFrameID)
	}
	if got.FrameTitle != "Inner" {
		t.Errorf("frameTitle = %q, want the frame's own title", got.FrameTitle)
	}
	// The live url wins over the stored one: the scope records the url the frame had when it
	// was set, and a frame that navigated since would be attributed to a document it no
	// longer holds.
	if got.FrameURL != "http://127.0.0.1:1/inner.html" {
		t.Errorf("frameUrl = %q, want the frame's url at read time", got.FrameURL)
	}
	if got.OwnerRef != "e3" || got.FrameName != "payment" {
		t.Errorf("disclosure lost what the stored scope knows: %+v", got)
	}
}

// A cross-origin or torn-down frame cannot be read. The disclosure is still the honest half
// of the answer — the read WAS scoped — so it is published with what the scope knows.
func TestFrameDisclosureSurvivesAFrameItCannotRead(t *testing.T) {
	h := newScopedHandlers(t, bridge.FrameScope{FrameID: "frame-1", FrameURL: "http://stored/"},
		&frameIdentityBridge{fail: true})

	got := h.frameDisclosureFor(context.Background(), "tab1", "frame-1")
	if got == nil {
		t.Fatal("an unreadable frame suppressed the disclosure entirely, so the read looks like a whole-document one")
	}
	if got.FrameURL != "http://stored/" || got.FrameTitle != "" {
		t.Errorf("disclosure = %+v, want the stored url and no invented title", got)
	}
}

// The header is the surface the CLI prints, so this is where a scoped read becomes visible to
// a human reading a terminal. url and title keep meaning the TAB's document in both.
func TestSnapshotHeadersMarkAScopedReadAndLeaveAnUnscopedOneAlone(t *testing.T) {
	const title, pageURL = "Outer", "http://127.0.0.1:18798/"
	scope := &frameDisclosure{FrameScope: bridge.FrameScope{FrameID: "886601397BFA0B332880152438BD0153", OwnerRef: "e3"}}

	unscopedCompact := snapshotCompactHeader(title, pageURL, 7, nil)
	if unscopedCompact != "# Outer | http://127.0.0.1:18798/ | 7 nodes" {
		t.Errorf("an unscoped compact header changed shape: %q", unscopedCompact)
	}
	scopedCompact := snapshotCompactHeader(title, pageURL, 3, scope)
	if scopedCompact == unscopedCompact {
		t.Error("the two headers are identical, so a scoped read is indistinguishable from a whole-document one")
	}
	if !strings.Contains(scopedCompact, "frame e3") {
		t.Errorf("scoped compact header = %q, want it to name the frame", scopedCompact)
	}
	if !strings.HasPrefix(scopedCompact, "# Outer | http://127.0.0.1:18798/ |") {
		t.Errorf("scoped compact header re-pointed title or url at the frame: %q", scopedCompact)
	}

	unscopedText := snapshotTextHeader(title, pageURL, 7, nil)
	if unscopedText != "# Outer\n# http://127.0.0.1:18798/\n# 7 nodes" {
		t.Errorf("an unscoped text header changed shape: %q", unscopedText)
	}
	if !strings.Contains(snapshotTextHeader(title, pageURL, 3, scope), "# frame e3\n") {
		t.Errorf("scoped text header does not name the frame: %q", snapshotTextHeader(title, pageURL, 3, scope))
	}
}

// The owner ref is preferred because it is the handle `pinchtab frame <target>` accepts; a
// raw frame id is not a target that command resolves, so a header carrying only the id names
// something the reader cannot act on. Without a known ref the id is still better than silence.
func TestTheHeaderMarkerPrefersTheHandleTheFrameCommandAccepts(t *testing.T) {
	withRef := &frameDisclosure{FrameScope: bridge.FrameScope{FrameID: "886601397BFA0B33", OwnerRef: "e3"}}
	if got := withRef.marker(); got != "frame e3" {
		t.Errorf("marker = %q, want the owner ref", got)
	}
	withoutRef := &frameDisclosure{FrameScope: bridge.FrameScope{FrameID: "886601397BFA0B332880152438BD0153"}}
	got := withoutRef.marker()
	if !strings.HasPrefix(got, "frame 886601397BFA") {
		t.Errorf("marker = %q, want it to name the frame id", got)
	}
	if len(got) > len("frame ")+16 {
		t.Errorf("marker = %q, want the id shortened for a header line", got)
	}
}

// The text envelope is the other half of the reported defect. Two reads of the same tab and
// url differ in the disclosure, not only in what the text says.
func TestTextEnvelopeDisclosesTheFrameOnlyWhenScoped(t *testing.T) {
	extraction := textExtraction{Text: "inner frame paragraph", Mode: extractionRaw}

	unscoped := decodeTextEnvelope(t, scopedTextResponseRecorder(t, extraction, -1, "", nil))
	if _, ok := unscoped["frame"]; ok {
		t.Errorf("an unscoped text read published a frame key: %v", unscoped["frame"])
	}

	scoped := decodeTextEnvelope(t, scopedTextResponseRecorder(t, extraction, -1, "", &frameDisclosure{
		FrameScope: bridge.FrameScope{FrameID: "frame-1", FrameURL: "http://127.0.0.1:1/inner.html", OwnerRef: "e3"},
		FrameTitle: "Inner",
	}))
	frame, ok := scoped["frame"].(map[string]any)
	if !ok {
		t.Fatalf("a scoped text read published no frame object: %v", scoped)
	}
	for key, want := range map[string]string{
		"frameId":    "frame-1",
		"frameUrl":   "http://127.0.0.1:1/inner.html",
		"frameTitle": "Inner",
		"ownerRef":   "e3",
	} {
		if got, _ := frame[key].(string); got != want {
			t.Errorf("frame[%q] = %q, want %q", key, got, want)
		}
	}
	// The tab document keeps its own fields: that is what stops `url` meaning two things
	// depending on state a reader cannot see.
	if scoped["url"] != unscoped["url"] || scoped["title"] != unscoped["title"] {
		t.Errorf("a scoped read changed the tab's url/title: %v vs %v", scoped["url"], unscoped["url"])
	}
}

// outerFixtureHTML and innerFixtureHTML mirror the reported fixture: two documents that are
// distinguishable only by their content, so a read attributed to the wrong one is visible.
const outerFixtureHTML = `<!doctype html><html><head><title>Outer</title></head><body>
<h1>OuterHeading</h1><input id="oh" value="outerval"><button>OuterBtn</button>
<iframe id="f" src="/inner.html" width="300" height="200"></iframe>
</body></html>`

const innerFixtureHTML = `<!doctype html><html><head><title>Inner</title></head><body>
<h1>InnerHeading</h1><input value="innerval"><button>InnerBtn</button>
<p>Inner frame paragraph text that is unique to the child document.</p>
</body></html>`

func newFrameDisclosureFixture(t *testing.T) (*Handlers, string, string) {
	t.Helper()
	chromePath := testbrowser.Path(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/inner.html", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(innerFixtureHTML))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(outerFixtureHTML))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.UserDataDir(testbrowser.ProfileDir(t)),
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
	)...)
	ctx, cancelBrowser := chromedp.NewContext(alloc)
	ctx, cancelTimeout := context.WithTimeout(ctx, 30*time.Second)
	t.Cleanup(func() {
		cancelTimeout()
		cancelBrowser()
		cancelAlloc()
	})

	if err := chromedp.Run(ctx, chromedp.Navigate(server.URL), chromedp.WaitVisible("#f", chromedp.ByID)); err != nil {
		t.Fatal(err)
	}

	cfg := &config.RuntimeConfig{ActionTimeout: 10 * time.Second, DefaultBrowser: config.BrowserChrome, StateDir: t.TempDir()}
	b := bridge.New(context.Background(), ctx, cfg)
	b.RegisterTab("tab-frame", ctx)
	return New(b, cfg, nil, nil, nil), "tab-frame", server.URL
}

func snapshotBody(t *testing.T, h *Handlers, tabID, format string) string {
	t.Helper()
	req := httptest.NewRequest("GET", "/snapshot?tabId="+tabID+"&format="+format, nil)
	w := httptest.NewRecorder()
	h.HandleSnapshot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("snapshot (%s): status %d body=%s", format, w.Code, w.Body.String())
	}
	return w.Body.String()
}

// The reported defect, end to end: the same tab at the same url read twice, once scoped and
// once not. Before this change the two answers differed only in the node count, which reads
// as a page change rather than a fragment.
func TestAScopedReadIsDistinguishableFromAWholeDocumentRead(t *testing.T) {
	h, tabID, pageURL := newFrameDisclosureFixture(t)

	unscopedHeader := snapshotBody(t, h, tabID, "compact")
	if !strings.Contains(unscopedHeader, "OuterHeading") || !strings.Contains(unscopedHeader, "InnerHeading") {
		t.Fatalf("the unscoped read did not cover both documents: %s", unscopedHeader)
	}
	if header := strings.SplitN(unscopedHeader, "\n", 2)[0]; strings.Contains(header, "frame ") {
		t.Errorf("an unscoped read claims a frame scope: %s", header)
	}

	frameReq := httptest.NewRequest("POST", "/frame?tabId="+tabID, strings.NewReader(`{"target":"inner.html"}`))
	frameRes := httptest.NewRecorder()
	h.HandleFrame(frameRes, frameReq)
	if frameRes.Code != http.StatusOK {
		t.Fatalf("frame scope: status %d body=%s", frameRes.Code, frameRes.Body.String())
	}

	scopedHeader := snapshotBody(t, h, tabID, "compact")
	if strings.Contains(scopedHeader, "OuterHeading") {
		t.Fatalf("the scope did not take effect, so this test proves nothing: %s", scopedHeader)
	}
	if !strings.Contains(strings.SplitN(scopedHeader, "\n", 2)[0], "frame ") {
		t.Errorf("the scoped header does not disclose the scope: %s", scopedHeader)
	}
	if !strings.Contains(scopedHeader, pageURL) {
		t.Errorf("the scoped header stopped reporting the tab's url: %s", scopedHeader)
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(snapshotBody(t, h, tabID, "")), &envelope); err != nil {
		t.Fatalf("decode scoped snapshot: %v", err)
	}
	if envelope["title"] != "Outer" || envelope["url"] != pageURL+"/" && envelope["url"] != pageURL {
		t.Errorf("a scoped read re-pointed the tab's own url/title: url=%v title=%v", envelope["url"], envelope["title"])
	}
	frame, ok := envelope["frame"].(map[string]any)
	if !ok {
		t.Fatalf("the scoped snapshot published no frame object: %v", envelope)
	}
	if frame["frameTitle"] != "Inner" {
		t.Errorf("frame.frameTitle = %v, want the child document's title", frame["frameTitle"])
	}
	if url, _ := frame["frameUrl"].(string); !strings.HasSuffix(url, "/inner.html") {
		t.Errorf("frame.frameUrl = %v, want the child document's url", frame["frameUrl"])
	}
	if id, _ := frame["frameId"].(string); id == "" {
		t.Error("frame.frameId is empty, so the disclosure names no frame")
	}

	// /text is the other endpoint the card measured, and it reaches the disclosure through a
	// different path — its frame comes from resolveTargetFrameID, not from the snapshot's
	// scope lookup — so the wiring is asserted here rather than inferred from the snapshot.
	textReq := httptest.NewRequest("GET", "/text?tabId="+tabID, nil)
	textRes := httptest.NewRecorder()
	h.HandleText(textRes, textReq)
	if textRes.Code != http.StatusOK {
		t.Fatalf("text: status %d body=%s", textRes.Code, textRes.Body.String())
	}
	var textEnvelope map[string]any
	if err := json.Unmarshal(textRes.Body.Bytes(), &textEnvelope); err != nil {
		t.Fatalf("decode scoped text: %v", err)
	}
	if text, _ := textEnvelope["text"].(string); !strings.Contains(text, "Inner frame paragraph") {
		t.Fatalf("the scoped text read did not return the child document: %q", text)
	}
	textFrame, ok := textEnvelope["frame"].(map[string]any)
	if !ok {
		t.Fatalf("the scoped text read published no frame object: %v", textEnvelope)
	}
	if textFrame["frameTitle"] != "Inner" {
		t.Errorf("text frame.frameTitle = %v, want the child document's title", textFrame["frameTitle"])
	}
	if textEnvelope["title"] != "Outer" {
		t.Errorf("the scoped text read re-pointed the tab's title: %v", textEnvelope["title"])
	}
}

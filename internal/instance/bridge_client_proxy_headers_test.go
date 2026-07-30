package instance

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/httpx"
)

func bridgeStub(t *testing.T) (*BridgeClient, string) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, name := range httpx.OuterChainResponseHeaders() {
			w.Header().Set(name, "instance-"+name)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	parsed, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return NewBridgeClient(), parsed.Port()
}

func assertOuterChainHeadersSurviveAlone(t *testing.T, header http.Header) {
	t.Helper()

	for _, name := range httpx.OuterChainResponseHeaders() {
		got := header.Values(name)
		if len(got) != 1 {
			t.Errorf("%s = %v, want exactly one value — the instance's own copy is a value the outer process never minted or logged", name, got)
			continue
		}
		if got[0] != "outer-"+name {
			t.Errorf("%s = %q, want the outer chain's value", name, got[0])
		}
	}
	if got := header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want the instance's own response header copied through", got)
	}
}

func recorderWithOuterChainHeaders() *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	for _, name := range httpx.OuterChainResponseHeaders() {
		rec.Header().Set(name, "outer-"+name)
	}
	return rec
}

func TestProxyWithTabIDDoesNotDoubleTheOuterChainsResponseHeaders(t *testing.T) {
	client, port := bridgeStub(t)

	rec := recorderWithOuterChainHeaders()
	req := httptest.NewRequest("POST", "/find", strings.NewReader(`{"text":"Buy"}`))

	client.ProxyWithTabID(rec, req, port, "tab-1", "/find")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	assertOuterChainHeadersSurviveAlone(t, rec.Header())
}

func TestProxyToTabDoesNotDoubleTheOuterChainsResponseHeaders(t *testing.T) {
	client, port := bridgeStub(t)

	rec := recorderWithOuterChainHeaders()
	req := httptest.NewRequest("GET", "/tabs/tab-1/text", nil)

	client.ProxyToTab(rec, req, port, "tab-1", "/text")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	assertOuterChainHeadersSurviveAlone(t, rec.Header())
}

func TestProxyToTabForwardsRequestHeadersButNotHopByHopOnes(t *testing.T) {
	var seen http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.WriteHeader(200)
	}))
	defer srv.Close()

	parsed, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/tabs/tab-1/text", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Te", "trailers")

	NewBridgeClient().ProxyToTab(httptest.NewRecorder(), req, parsed.Port(), "tab-1", "/text")

	if got := seen.Get("Authorization"); got != "Bearer token" {
		t.Errorf("Authorization = %q, want it forwarded to the instance", got)
	}
	if got := seen.Get("Te"); got != "" {
		t.Errorf("Te = %q, want the hop-by-hop header dropped on the proxy hop", got)
	}
}

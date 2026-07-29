package staticfetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/pinchtab/pinchtab/internal/sanitize"
)

// The accessible name is the identity an agent matches on to decide what to
// click, so a cut that lands inside a rune does not merely look wrong — it puts
// invalid UTF-8 into a JSON response and renders as U+FFFD.
//
// The label is 99 ASCII bytes followed by a two-byte rune, so the byte cap falls
// between that rune's two bytes. A hand-rolled text[:100] keeps the lead byte
// alone; a rune-aware cut cannot.
func TestAccessibleNameIsTruncatedOnARuneBoundary(t *testing.T) {
	label := strings.Repeat("A", accessibleNameMaxBytes-1) + "é"
	html := `<!doctype html><html><body><button>` + label + `</button></body></html>`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	}))
	defer ts.Close()

	lite := NewBrowser()
	defer func() { _ = lite.Close() }()
	if _, err := lite.Navigate(context.Background(), ts.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	var name string
	for _, node := range snapshotNodes(t, lite, "all") {
		if strings.HasPrefix(node.Name, "AAAA") {
			name = node.Name
			break
		}
	}
	if name == "" {
		t.Fatal("no snapshot node carries the long button label — the fixture no longer reaches getAccessibleName")
	}

	if !utf8.ValidString(name) {
		t.Errorf("accessible name is not valid UTF-8: %q", name)
	}
	if strings.ContainsRune(name, utf8.RuneError) {
		t.Errorf("accessible name carries the replacement character: %q", name)
	}
	if !strings.HasSuffix(name, sanitize.TruncationSuffix) {
		t.Errorf("accessible name %q does not carry the truncation marker %q", name, sanitize.TruncationSuffix)
	}
	if len(name) > accessibleNameMaxBytes {
		t.Errorf("accessible name is %d bytes, over the %d-byte total budget: %q", len(name), accessibleNameMaxBytes, name)
	}
}

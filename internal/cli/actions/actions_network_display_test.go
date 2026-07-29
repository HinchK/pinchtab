package actions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/pinchtab/pinchtab/internal/sanitize"
	"github.com/spf13/cobra"
)

func newNetworkTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("clear", false, "")
	cmd.Flags().Bool("stream", false, "")
	cmd.Flags().Bool("json", false, "")
	for _, name := range []string{"tab", "filter", "method", "status", "type", "limit", "buffer-size"} {
		cmd.Flags().String(name, "", "")
	}
	return cmd
}

// The URL column is a byte budget, so a percent-decoded non-ASCII URL longer than
// the cap must still be cut on a rune boundary — a terminal shows U+FFFD for a
// half rune, and the listing is what an operator reads to pick a requestId.
func TestNetworkListingTruncatesURLsOnARuneBoundary(t *testing.T) {
	longURL := "https://example.com/" + strings.Repeat("a", networkURLDisplayMaxBytes) + "é/tail"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"count": 1,
			"entries": []map[string]any{
				{"method": "GET", "status": 200, "url": longURL},
			},
		})
	}))
	defer srv.Close()

	out := captureStdout(t, func() {
		Network(http.DefaultClient, srv.URL, "", newNetworkTestCmd(), nil)
	})

	if !utf8.ValidString(out) {
		t.Errorf("network listing is not valid UTF-8: %q", out)
	}
	if strings.ContainsRune(out, utf8.RuneError) {
		t.Errorf("network listing carries the replacement character: %q", out)
	}

	line := strings.TrimSpace(out)
	shown := line[strings.LastIndex(line, "  ")+2:]
	if !strings.HasSuffix(shown, sanitize.TruncationSuffix) {
		t.Errorf("displayed URL %q does not carry the truncation marker", shown)
	}
	if len(shown) != networkURLDisplayMaxBytes {
		t.Errorf("displayed URL is %d bytes, want the %d-byte total budget: %q", len(shown), networkURLDisplayMaxBytes, shown)
	}
}

// A URL inside the budget must be printed whole, or the conversion would have
// traded a mid-rune cut for a truncation that fires too early.
func TestNetworkListingLeavesAShortURLIntact(t *testing.T) {
	const shortURL = "https://example.com/ok"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"count":   1,
			"entries": []map[string]any{{"method": "GET", "status": 200, "url": shortURL}},
		})
	}))
	defer srv.Close()

	out := captureStdout(t, func() {
		Network(http.DefaultClient, srv.URL, "", newNetworkTestCmd(), nil)
	})
	if !strings.Contains(out, shortURL) {
		t.Errorf("listing %q does not contain the untruncated URL %q", out, shortURL)
	}
}

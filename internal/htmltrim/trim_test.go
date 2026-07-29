package htmltrim

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTrimHTMLStripsScriptsAndStyles(t *testing.T) {
	html := `<html><head><style>body{color:red}</style><script>var secret=1;</script></head>` +
		`<body><!-- a comment --><svg><path d="M0 0"/></svg><img src="data:image/png;base64,AAAA">` +
		`<button id="go">Go</button></body></html>`

	got := TrimHTML(html)

	for _, unwanted := range []string{"<style", "<script", "color:red", "var secret", "<!--", "<svg", "base64"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("trimmed HTML still contains %q:\n%s", unwanted, got)
		}
	}
	if !strings.Contains(got, `<button id="go">Go</button>`) {
		t.Errorf("trimmed HTML dropped the interactive element:\n%s", got)
	}
}

func TestTrimHTMLCapsOnRuneBoundary(t *testing.T) {
	body := strings.Repeat("é", maxTrimmedBytes)
	got := TrimHTML("<p>" + body + "</p>")

	if len(got) > maxTrimmedBytes {
		t.Fatalf("trimmed HTML is %d bytes, over the %d-byte cap", len(got), maxTrimmedBytes)
	}
	if len(got) < maxTrimmedBytes-utf8.UTFMax {
		t.Fatalf("trimmed HTML is %d bytes, far under the %d-byte cap", len(got), maxTrimmedBytes)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("trimmed HTML is not valid UTF-8: %q", got[len(got)-8:])
	}
	if strings.ContainsRune(got, utf8.RuneError) {
		t.Fatalf("trimmed HTML contains U+FFFD absent from the source")
	}
	if strings.HasSuffix(got, ".") {
		t.Fatalf("trimmed HTML got a truncation marker appended: %q", got[len(got)-8:])
	}
	if !strings.HasPrefix("<p>"+body+"</p>", got) {
		t.Fatalf("trimmed HTML is not a byte-exact prefix of the source")
	}
}

func TestTrimHTMLLeavesShortInputUncapped(t *testing.T) {
	html := "<p>héllo</p>"
	if got := TrimHTML(html); got != html {
		t.Fatalf("TrimHTML(%q) = %q, want it unchanged", html, got)
	}
}

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

// Stripping has to happen before the cap, and neither of the assertions above can
// tell. Cap first and no script survives (stripping still runs) and the result is
// still under the cap — both stay green while the prompt budget is spent on
// content that is then thrown away, which is the token waste this package exists
// to remove. On a page whose interactive markup sits after a large script or style
// block, that markup is not merely crowded out, it is absent: the model is asked
// to act on a page with no form in it.
func TestTrimHTMLSpendsTheBudgetOnMarkupNotOnStrippedContent(t *testing.T) {
	html := "<html><head><style>" + strings.Repeat("body{color:red}", 200) +
		"</style><script>" + strings.Repeat("var x=1;doSomething();", 300) +
		"</script></head><body>" +
		`<form><input id="user" name="user"><input id="pass" type="password">` +
		`<button id="go">Sign in</button></form>` +
		"</body></html>"

	if len(html) <= maxTrimmedBytes {
		t.Fatalf("fixture is %d bytes, must exceed the %d-byte cap or the ordering is not exercised", len(html), maxTrimmedBytes)
	}

	got := TrimHTML(html)

	// The interactive markup is the whole reason the prompt carries HTML at all.
	for _, want := range []string{`id="go"`, `type="password"`, `id="user"`} {
		if !strings.Contains(got, want) {
			t.Errorf("trimmed HTML lost %s — the cap was applied before stripping, so the budget went on script and style that were then discarded:\n%s", want, got)
		}
	}
}

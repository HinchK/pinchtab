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

// A base64 image is usually the single largest token contributor on a page, and the cap
// is applied AFTER stripping — so one payload that survives spends the whole prompt
// budget on a picture and pushes the interactive markup off the end. The old pattern was
// anchored on the whole attribute value, so it only ever saw src="data:…" and missed the
// commonest carrier of all: a URL inside an inline style declaration.
func TestTrimHTMLStripsADataURIEmbeddedInAValue(t *testing.T) {
	const payload = "QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo="

	for _, tc := range []struct {
		name string
		html string
		want string
	}{
		{
			name: "css url, unquoted",
			html: `<div style="background:url(data:image/png;base64,` + payload + `)">x</div>`,
			want: `<div style="background:url()">x</div>`,
		},
		{
			name: "css url, single-quoted",
			html: `<div style="background-image:url('data:image/png;base64,` + payload + `')">x</div>`,
			want: `<div style="background-image:url('')">x</div>`,
		},
		{
			name: "css url among other declarations",
			html: `<div style="color:red;background:url(data:image/gif;base64,` + payload + `);border:1px">x</div>`,
			want: `<div style="color:red;background:url();border:1px">x</div>`,
		},
		{
			name: "whole value, the already-covered form",
			html: `<img src="data:image/png;base64,` + payload + `">`,
			want: `<img src="">`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := TrimHTML(tc.html)
			if strings.Contains(got, payload) {
				t.Errorf("the base64 payload survived:\n%s", got)
			}
			if strings.Contains(got, "data:") {
				t.Errorf("the data URI survived:\n%s", got)
			}
			if got != tc.want {
				t.Errorf("TrimHTML =\n  %s\nwant\n  %s", got, tc.want)
			}
		})
	}
}

// The widened pattern must not regress the forms that already stripped. srcset carries
// several URIs in one value separated by commas, and a comma also separates a data URI's
// own metadata from its payload — so the pattern cannot treat commas as a terminator,
// and this is the case that proves it does not.
func TestTrimHTMLStillStripsWholeValueDataURIs(t *testing.T) {
	html := `<img srcset="data:image/png;base64,AAAABBBBCCCC 1x, data:image/png;base64,DDDDEEEEFFFF 2x" alt="pic">`

	got := TrimHTML(html)

	for _, unwanted := range []string{"data:", "base64", "AAAABBBBCCCC", "DDDDEEEEFFFF"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("srcset still carries %q:\n%s", unwanted, got)
		}
	}
	// The descriptors and the rest of the tag are markup, not payload, and stay.
	for _, want := range []string{"1x", "2x", `alt="pic"`, "<img"} {
		if !strings.Contains(got, want) {
			t.Errorf("srcset stripping removed %q, which is markup rather than payload:\n%s", want, got)
		}
	}
}

// A URI opening an element's text content is preceded by '>', not by a quote, paren, comma,
// '=' or whitespace. Licensing only those delimiters missed it and the whole blob survived
// — the budget cost this helper exists to prevent, reintroduced by the guard against
// over-matching. Text position is where a URI is most expensive, since nothing else in the
// pipeline strips it.
func TestTrimHTMLStripsADataURIOpeningTextContent(t *testing.T) {
	blob := strings.Repeat("A", 64)

	for _, tc := range []struct{ name, html string }{
		{"table cell", `<td>data:image/png;base64,` + blob + `</td>`},
		{"code block", `<code>data:text/html,hello</code>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := TrimHTML(tc.html)
			for _, unwanted := range []string{"data:", blob, "text/html"} {
				if strings.Contains(got, unwanted) {
					t.Errorf("still carries %q, so a URI in text position costs the whole budget:\n%s", unwanted, got)
				}
			}
		})
	}
}

// The data-URI pattern strips a URI, not the letters "data:" wherever they fall. Widening
// it to match anywhere inside a value also let it match inside a word — "Metadata:" in page
// text became "Meta", eating the interactive markup this helper exists to carry. A real URI
// is preceded by a quote, paren, comma, '=', whitespace or '>', so those are what license
// the strip. Reverting either guard corrupts one of these.
func TestTrimHTMLLeavesDataColonInsideAWordAlone(t *testing.T) {
	for _, tc := range []struct{ name, html, want string }{
		{"prose label", `<p>Metadata: 2024</p>`, `<p>Metadata: 2024</p>`},
		{"word with a mime tail", `<td>metadata:image/png here</td>`, `<td>metadata:image/png here</td>`},
		{"data- attribute is unaffected", `<div data-src="keep">x</div>`, `<div data-src="keep">x</div>`},
		// A field label opening a cell has a delimiter before it — the '>' that closes the
		// tag — so the delimiter guard cannot save it. What does is the payload shape: a
		// label carries no MIME prefix and no '/', ';' or ','.
		{"bare label opening a cell", `<td>Data:</td>`, `<td>Data:</td>`},
		{"labelled value opening a cell", `<td>Data: 42</td>`, `<td>Data: 42</td>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := TrimHTML(tc.html); got != tc.want {
				t.Errorf("TrimHTML(%q) = %q, want it unchanged — 'data:' inside a word is not a URI", tc.html, got)
			}
		})
	}
}

// SVG is foreign content and nests legally, so a nested <svg> reaches this helper through
// DOM serialisation. The non-greedy regex stopped at the FIRST </svg>, leaving the outer
// element's path data and a stray closing tag in the prompt.
func TestTrimHTMLStripsNestedSVGWhole(t *testing.T) {
	got := TrimHTML(`<svg>a<svg>b</svg>PATHDATA</svg>tail`)

	for _, unwanted := range []string{"<svg", "</svg", "PATHDATA"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("nested SVG left %q behind: %q", unwanted, got)
		}
	}
	if got != "tail" {
		t.Errorf("TrimHTML = %q, want %q", got, "tail")
	}
}

// Depth counting must not become a greedy match: greedy runs to the LAST </svg> in the
// document and would swallow every interactive element between two sibling icons, which
// is worse than the nesting bug it fixes.
func TestTrimHTMLKeepsMarkupBetweenSiblingSVGs(t *testing.T) {
	got := TrimHTML(`<svg><path d="M0 0"/></svg><button id="go">Go</button><svg><circle r="1"/></svg>`)

	if !strings.Contains(got, `<button id="go">Go</button>`) {
		t.Errorf("markup between two sibling SVGs was swallowed: %q", got)
	}
	for _, unwanted := range []string{"<svg", "</svg", "<path", "<circle"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("sibling SVG left %q behind: %q", unwanted, got)
		}
	}
}

// An element whose name merely starts with "svg" is ordinary markup and must survive; the
// boundary check is what separates <svg> from <svgicon>.
func TestTrimHTMLKeepsElementsMerelyNamedLikeSVG(t *testing.T) {
	html := `<svgicon id="keep">label</svgicon>`
	if got := TrimHTML(html); got != html {
		t.Errorf("TrimHTML(%q) = %q, want it unchanged", html, got)
	}
}

// PINNED BOUNDARY, not a defect: a raw-text element ends at the FIRST </script per the
// HTML tokenizer, so the tail after it is page-visible text that the browser renders too
// — golang.org/x/net/html picks byte-for-byte the same boundary. Do not "fix" this.
func TestTrimHTMLLeavesTheTailAfterAScriptStringLiteral(t *testing.T) {
	got := TrimHTML(`<p>before</p><script>var s="</script>";var secret=2;</script><p>after</p>`)

	if strings.Contains(got, "var s=") {
		t.Errorf("script content reached the prompt: %q", got)
	}
	if !strings.Contains(got, `";var secret=2;`) {
		t.Errorf("the tail is page text the browser also renders and must survive: %q", got)
	}
	for _, want := range []string{"<p>before</p>", "<p>after</p>"} {
		if !strings.Contains(got, want) {
			t.Errorf("surrounding markup lost %q: %q", want, got)
		}
	}
}

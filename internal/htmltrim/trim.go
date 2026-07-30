// Package htmltrim reduces page HTML to what an LLM prompt needs: the
// interactive markup, without the scripts, styles and binary payloads that
// dominate a full page's token cost.
package htmltrim

import (
	"regexp"
	"strings"

	"github.com/pinchtab/pinchtab/internal/sanitize"
)

// maxTrimmedBytes caps the result in bytes, cut on a rune boundary so the
// prompt is a byte-exact prefix of valid UTF-8.
const maxTrimmedBytes = 4000

// TrimHTML strips scripts, styles, comments, SVG, data URIs and excess
// whitespace, then caps the result at maxTrimmedBytes.
func TrimHTML(html string) string {
	html = reScript.ReplaceAllString(html, "")
	html = reStyle.ReplaceAllString(html, "")
	html = reComment.ReplaceAllString(html, "")
	html = stripSVG(html)
	html = reDataURI.ReplaceAllString(html, "")
	html = reWhitespace.ReplaceAllString(html, " ")
	html = reNewlines.ReplaceAllString(html, "\n")

	var trimmed []string
	for _, line := range strings.Split(html, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			trimmed = append(trimmed, line)
		}
	}

	return sanitize.PrefixUTF8Bytes(strings.Join(trimmed, "\n"), maxTrimmedBytes)
}

var (
	reScript  = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reStyle   = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	// reDataURI matches the URI itself, not the attribute value holding it. Anchored on
	// the whole value ("data:[^"]*") it saw only src="data:…" and missed the shape that
	// actually costs the budget, style="background:url(data:…)", where the URI is in the
	// middle of a declaration. Replacing just the URI is also what keeps that declaration
	// readable — url() rather than a blanked style attribute.
	//
	// The terminator set is what ends a URI in each host syntax: the quote of the
	// attribute, the quote or paren of a CSS url(), and whitespace for a srcset
	// candidate. A comma is NOT a terminator — it separates a data URI's own metadata
	// from its payload, so excluding it would leave every base64 blob behind.
	reDataURI    = regexp.MustCompile(`(?i)data:[^"'()<>\s]*`)
	reWhitespace = regexp.MustCompile(`[ \t]+`)
	reNewlines   = regexp.MustCompile(`\n{3,}`)
)

// stripSVG removes every <svg> element, counting depth so a nested one does not end the
// outer. The non-greedy regex it replaces stopped at the FIRST </svg>, leaving the outer
// element's remaining path data and a stray </svg> in the prompt; SVG is foreign content
// and nests legally, so that shape survives DOM serialisation and reaches the model.
//
// A greedy regex would fix nesting and break something worse: it runs to the LAST </svg>
// in the document, so two sibling icons would swallow every interactive element between
// them — the markup this package exists to preserve.
func stripSVG(html string) string {
	lower := strings.ToLower(html)
	var b strings.Builder
	for i := 0; ; {
		start := indexSVGOpen(lower, i)
		if start < 0 {
			b.WriteString(html[i:])
			return b.String()
		}
		b.WriteString(html[i:start])
		end := svgElementEnd(lower, start)
		if end < 0 {
			// Unterminated, which fragment serialisation cannot produce. The regex left
			// such input alone; so does this, rather than deleting the rest of the page.
			b.WriteString(html[start:])
			return b.String()
		}
		i = end
	}
}

func indexSVGOpen(lower string, from int) int {
	for i := from; ; {
		next := strings.Index(lower[i:], "<svg")
		if next < 0 {
			return -1
		}
		at := i + next
		if isSVGTagBoundary(lower, at+len("<svg")) {
			return at
		}
		i = at + len("<svg")
	}
}

// isSVGTagBoundary keeps <svg> and <svg ...> apart from an element merely starting with
// those letters, so <svgIcon> is left in the prompt as the ordinary markup it is.
func isSVGTagBoundary(lower string, at int) bool {
	if at >= len(lower) {
		return false
	}
	switch lower[at] {
	case '>', '/', ' ', '\t', '\n', '\r':
		return true
	}
	return false
}

// svgElementEnd returns the offset just past the </svg> that closes the element opening
// at start, or -1 if the document never closes it.
func svgElementEnd(lower string, start int) int {
	depth := 0
	for i := start; i < len(lower); {
		switch {
		case strings.HasPrefix(lower[i:], "<svg") && isSVGTagBoundary(lower, i+len("<svg")):
			tagClose := strings.IndexByte(lower[i:], '>')
			if tagClose < 0 {
				return -1
			}
			tagEnd := i + tagClose + 1
			if strings.HasSuffix(strings.TrimSpace(lower[i:i+tagClose]), "/") {
				if depth == 0 {
					return tagEnd
				}
			} else {
				depth++
			}
			i = tagEnd
		case strings.HasPrefix(lower[i:], "</svg"):
			tagClose := strings.IndexByte(lower[i:], '>')
			if tagClose < 0 {
				return -1
			}
			depth--
			i += tagClose + 1
			if depth <= 0 {
				return i
			}
		default:
			i++
		}
	}
	return -1
}

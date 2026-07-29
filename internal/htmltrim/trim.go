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
	html = reSVG.ReplaceAllString(html, "")
	html = reDataURI.ReplaceAllString(html, `""`)
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
	reScript     = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reStyle      = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reComment    = regexp.MustCompile(`(?s)<!--.*?-->`)
	reSVG        = regexp.MustCompile(`(?is)<svg[^>]*>.*?</svg>`)
	reDataURI    = regexp.MustCompile(`"data:[^"]*"`)
	reWhitespace = regexp.MustCompile(`[ \t]+`)
	reNewlines   = regexp.MustCompile(`\n{3,}`)
)

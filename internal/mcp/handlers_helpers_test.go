package mcp

import "testing"

// looksLikeStructuredSelector decides whether an MCP client's free-form `query`
// is passed through as a selector or wrapped as semantic `find:` text. Anything
// passed through is parsed as CSS by default, so a false positive turns natural
// language into an unmatchable selector. These cases pin the current boundary.
func TestLooksLikeStructuredSelector(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"plain label", "Sign In", false},
		{"plain sentence", "accept all cookies", false},
		{"decimal number", "50.50", false},
		{"id selector", "#submit", true},
		{"class selector", ".btn-primary", true},
		{"attribute selector", "[data-test=submit]", true},
		{"xpath", "//button[@id='ok']", true},
		{"parenthesised xpath", "(//a)[1]", true},
		{"descendant combinator", "div > p", true},
		{"tag dot class", "button.primary", true},

		// Current behaviour: the punctuation test on line 92 has no
		// whitespace guard, unlike the dot-notation branch below it, so
		// prose containing these characters is routed to CSS.
		{"prose with colon", "Sign up: it's free", true},
		{"prose with equals", "name = value", true},
		{"prose with plus", "Add + remove", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := looksLikeStructuredSelector(tt.in); got != tt.want {
				t.Errorf("looksLikeStructuredSelector(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestHasKnownSelectorPrefixIsCaseInsensitive(t *testing.T) {
	for _, in := range []string{"css:#a", "CSS:#a", "Find:submit", "NTH:2:div", "  text:hello"} {
		if !hasKnownSelectorPrefix(in) {
			t.Errorf("hasKnownSelectorPrefix(%q) = false, want true", in)
		}
	}
	for _, in := range []string{"submit", "unknownprefix:value"} {
		if hasKnownSelectorPrefix(in) {
			t.Errorf("hasKnownSelectorPrefix(%q) = true, want false", in)
		}
	}
}

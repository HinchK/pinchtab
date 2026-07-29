package mcp

import (
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

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
		{"pseudo class colon has no surrounding space", "a:hover", true},
		{"attribute equals has no surrounding space", "input[type=text]", true},
		{"attribute selector with sibling combinator", "input[name=q] + label", true},

		{"prose colon is space separated", "Sign up: it's free", false},
		{"prose equals is space separated", "name = value", false},
		{"colon with a space only before it", "ready :go", false},
		{"equals with a space only after it", "name= value", false},
		{"prose plus stays CSS because combinators are space separated in CSS too", "Add + remove", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := looksLikeStructuredSelector(tt.in); got != tt.want {
				t.Errorf("looksLikeStructuredSelector(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestActionSelectorArgUsesTheGrammarsPrefixVocabulary(t *testing.T) {
	// "find: login button" and "Role: Save" are the discriminating cases: the
	// space after the colon keeps looksLikeStructuredSelector from claiming
	// them, so only the grammar's prefix vocabulary passes them through.
	for _, in := range []string{"css:#a", "CSS:#a", "Find:submit", "NTH:2:div", "  text:hello", "find: login button", "Role: Save"} {
		want := strings.TrimSpace(in)
		if got := actionSelectorArg(queryRequest(in)); got != want {
			t.Errorf("actionSelectorArg(%q) = %q, want %q passed through as a selector", in, got, want)
		}
	}
	for _, in := range []string{"submit", "unknownprefix value"} {
		if got := actionSelectorArg(queryRequest(in)); got != "find:"+in {
			t.Errorf("actionSelectorArg(%q) = %q, want it wrapped as find:", in, got)
		}
	}
}

func queryRequest(query string) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": query}
	return req
}

func TestActionSelectorArgRoutesSpaceSeparatedProseToFind(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{"prose colon is space separated", "Sign up: it's free", "find:Sign up: it's free"},
		{"prose equals is space separated", "name = value", "find:name = value"},
		{"pseudo class colon has no surrounding space", "a:hover", "a:hover"},
		{"attribute equals has no surrounding space", "input[type=text]", "input[type=text]"},
		{"attribute selector with sibling combinator", "input[name=q] + label", "input[name=q] + label"},
		{"descendant combinator", "div > p", "div > p"},
		{"prose plus stays CSS because combinators are space separated in CSS too", "Add + remove", "Add + remove"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = map[string]any{"query": tt.query}
			if got := actionSelectorArg(req); got != tt.want {
				t.Errorf("actionSelectorArg(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}

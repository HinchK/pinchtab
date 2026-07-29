package mcp

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/pinchtab/pinchtab/internal/selector"
)

func optString(r mcp.CallToolRequest, key string) string {
	v, _ := r.GetArguments()[key].(string)
	return v
}

func optTrimmedString(r mcp.CallToolRequest, key string) string {
	return strings.TrimSpace(optString(r, key))
}

func optFloat(r mcp.CallToolRequest, key string) (float64, bool) {
	v, ok := r.GetArguments()[key].(float64)
	return v, ok
}

func optInt(r mcp.CallToolRequest, key string) (int, bool) {
	if v, ok := optFloat(r, key); ok {
		return int(v), true
	}
	if raw := optTrimmedString(r, key); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			return n, true
		}
	}
	return 0, false
}

func optBool(r mcp.CallToolRequest, key string) (bool, bool) {
	v, ok := r.GetArguments()[key].(bool)
	return v, ok
}

func optNumber(r mcp.CallToolRequest, key string) float64 {
	v, _ := r.GetArguments()[key].(float64)
	return v
}

func formatInt(v float64) string {
	return fmt.Sprintf("%d", int(v))
}

func firstNonEmptyString(r mcp.CallToolRequest, keys ...string) string {
	for _, key := range keys {
		if v := optTrimmedString(r, key); v != "" {
			return v
		}
	}
	return ""
}

func hasKnownSelectorPrefix(v string) bool {
	lower := strings.ToLower(strings.TrimSpace(v))
	return strings.HasPrefix(lower, "css:") ||
		strings.HasPrefix(lower, "xpath:") ||
		strings.HasPrefix(lower, "text:") ||
		strings.HasPrefix(lower, "find:") ||
		strings.HasPrefix(lower, "semantic:") ||
		strings.HasPrefix(lower, "role:") ||
		strings.HasPrefix(lower, "label:") ||
		strings.HasPrefix(lower, "placeholder:") ||
		strings.HasPrefix(lower, "alt:") ||
		strings.HasPrefix(lower, "title:") ||
		strings.HasPrefix(lower, "testid:") ||
		strings.HasPrefix(lower, "first:") ||
		strings.HasPrefix(lower, "last:") ||
		strings.HasPrefix(lower, "nth:") ||
		strings.HasPrefix(lower, "ref:")
}

func looksLikeStructuredSelector(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	if strings.HasPrefix(v, "#") || strings.HasPrefix(v, ".") || strings.HasPrefix(v, "[") {
		return true
	}
	if strings.HasPrefix(v, "//") || strings.HasPrefix(v, "(//") {
		return true
	}
	if strings.ContainsAny(v, "[]#>+~") || containsSpacelessAny(v, ":=") {
		return true
	}
	// Treat dot notation as CSS only when it looks like tag/class syntax,
	// not plain text like numeric values (e.g. "50.50").
	if strings.Contains(v, ".") && hasASCIIAlpha(v) && !strings.ContainsAny(v, " \t\r\n") {
		return true
	}
	return false
}

func containsSpacelessAny(v, chars string) bool {
	for i := 0; i < len(v); i++ {
		if !strings.ContainsRune(chars, rune(v[i])) {
			continue
		}
		if i > 0 && isASCIISpace(v[i-1]) {
			continue
		}
		if i+1 < len(v) && isASCIISpace(v[i+1]) {
			continue
		}
		return true
	}
	return false
}

func isASCIISpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

func hasASCIIAlpha(v string) bool {
	for i := 0; i < len(v); i++ {
		c := v[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return true
		}
	}
	return false
}

// actionSelectorArg resolves common selector aliases used by MCP clients.
// If only "query" is provided, natural language input is normalized to
// semantic selector form (find:...).
func actionSelectorArg(r mcp.CallToolRequest) string {
	if sel := firstNonEmptyString(r, "selector", "ref", "element", "target"); sel != "" {
		return sel
	}
	query := optTrimmedString(r, "query")
	if query == "" {
		return ""
	}
	if hasKnownSelectorPrefix(query) || selector.IsRef(query) || looksLikeStructuredSelector(query) {
		return query
	}
	return "find:" + query
}

func resolveXY(r mcp.CallToolRequest) (float64, float64, bool) {
	x, okX := optFloat(r, "x")
	y, okY := optFloat(r, "y")
	if okX && okY {
		return x, y, true
	}
	return 0, 0, false
}

func resultFromBytes(body []byte, code int) (*mcp.CallToolResult, error) {
	if code >= 400 {
		return mcp.NewToolResultError(fmt.Sprintf("HTTP %d: %s", code, string(body))), nil
	}
	return mcp.NewToolResultText(string(body)), nil
}

type profileInstanceStatus struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
	Status  string `json:"status"`
	Port    string `json:"port"`
	ID      string `json:"id"`
	Error   string `json:"error"`
}

func jsonResult(v any) (*mcp.CallToolResult, error) {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("encode response: %v", err)), nil
	}
	return mcp.NewToolResultText(string(body)), nil
}

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

// Tool calls are written by language models, and a stringified number is one of
// the commonest shapes they emit. Every numeric argument accepts both, so a
// future one cannot pick a strict accessor by accident — there is none.
func optFloat(r mcp.CallToolRequest, key string) (float64, bool) {
	if v, ok := r.GetArguments()[key].(float64); ok {
		return v, true
	}
	if raw := optTrimmedString(r, key); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			return v, true
		}
	}
	return 0, false
}

func optInt(r mcp.CallToolRequest, key string) (int, bool) {
	v, ok := optFloat(r, key)
	return int(v), ok
}

// Booleans get the same tolerance as numbers, and for the same reason. The one
// opt-out argument makes it matter more than the opt-in flags: dropping
// withBounds="false" leaves bounds switched on, so the response looks like the
// default rather than like the request.
func optBool(r mcp.CallToolRequest, key string) (bool, bool) {
	if v, ok := r.GetArguments()[key].(bool); ok {
		return v, true
	}
	if raw := optTrimmedString(r, key); raw != "" {
		if v, err := strconv.ParseBool(raw); err == nil {
			return v, true
		}
	}
	return false, false
}

func firstNonEmptyString(r mcp.CallToolRequest, keys ...string) string {
	for _, key := range keys {
		if v := optTrimmedString(r, key); v != "" {
			return v
		}
	}
	return ""
}

// firstSuppliedString answers "was this argument sent", the question firstNonEmptyString
// cannot answer because collapsing empty and absent is its entire job. It is the same
// question the bridge asks — ActionRequest.HasText is inferred from key presence over the
// same text/value pair that fill and select both read.
//
// The value travels verbatim rather than trimmed, so MCP and POST /action agree on every
// input and not merely on the cases where emptiness is the point.
//
// The dialog arguments keep the collapsing helper deliberately: an empty dialogAction means
// not-specified, so a value this helper would report as supplied-but-unusable is instead a
// skipped one-shot handler. That is a silent SKIP rather than a misleading refusal — a
// different shape, and it wants its own decision rather than being swept in here.
//
// wrongType names the JSON type of a value that WAS supplied under one of the keys but is
// not a string. The library does not enforce the declared type before calling a handler, so
// a model answering a numeric-looking field with 2024 rather than "2024" arrives here — and
// collapsing that into "not supplied" reproduces the very complaint this helper was written
// for: telling a caller an argument it sent is missing.
func firstSuppliedString(r mcp.CallToolRequest, keys ...string) (value string, supplied bool, wrongType string) {
	args := r.GetArguments()
	for _, key := range keys {
		raw, ok := args[key]
		if !ok {
			continue
		}
		if v, isString := raw.(string); isString {
			return v, true, ""
		}
		if wrongType == "" {
			wrongType = jsonTypeName(raw)
		}
	}
	return "", false, wrongType
}

func jsonTypeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64, int, int64:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", v)
	}
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
	if selector.HasKnownPrefix(query) || selector.IsRef(query) || looksLikeStructuredSelector(query) {
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

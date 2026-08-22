package mcp

import (
	"net/url"
	"testing"

	"github.com/pinchtab/pinchtab/internal/handlers"
)

// normalizeSnapshotFormat used to be the only place a bad format was caught, so the
// guarantee reached MCP callers and nobody else. The HTTP handler enforces it now, which
// makes this layer an early, friendlier rejection rather than the enforcement — and that
// only holds while the two agree on what is valid. A format MCP accepts and the handler
// rejects would turn a clear tool error into a 400 from underneath.
func TestMCPAcceptsOnlyFormatsTheHandlerAlsoAccepts(t *testing.T) {
	for _, candidate := range []string{"compact", "text", "json", "yaml", "  TEXT  ", "xml", "compct", ""} {
		normalized, mcpErr := normalizeSnapshotFormat(candidate)
		if mcpErr != nil {
			continue
		}
		if _, err := handlers.ParseSnapshotCostControls(url.Values{"format": {normalized}}); err != nil {
			t.Errorf("MCP accepts format %q (as %q) but the handler rejects it: %v", candidate, normalized, err)
		}
	}
}

// The inverse is deliberately NOT required: the handler serves json and yaml, which the MCP
// tools do not offer. This pins that the narrowing is the only difference, so a future
// format added to the handler does not quietly become unreachable from MCP by accident.
func TestMCPNarrowsTheHandlerRatherThanDivergingFromIt(t *testing.T) {
	for _, format := range []string{"compact", "text"} {
		if _, err := normalizeSnapshotFormat(format); err != nil {
			t.Errorf("MCP rejects %q, which its own tools document: %v", format, err)
		}
	}
	for _, handlerOnly := range []string{"json", "yaml"} {
		if _, err := normalizeSnapshotFormat(handlerOnly); err == nil {
			t.Errorf("MCP now accepts %q; if that is intended, the tool schema and this test both need updating", handlerOnly)
		}
	}
}

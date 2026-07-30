package handlers

import (
	"strings"
	"testing"
)

// The allowlist guidance used to be appended to the error MESSAGE. It now lives
// in details.remedy, which the CLI renders on its own line — but it has to keep
// the properties that made it usable: name the host, append to the current value
// instead of replacing it, and carry no placeholder a user would paste literally.
func TestIDPIAllowlistRemedy(t *testing.T) {
	remedy := idpiAllowlistRemedy("https://example.com/some/path")
	if !strings.Contains(remedy, "example.com") {
		t.Errorf("remedy should name the blocked host; got %q", remedy)
	}
	if !strings.Contains(remedy, "security.allowedDomains") {
		t.Errorf("remedy should name the allowlist config key; got %q", remedy)
	}
	if !strings.Contains(remedy, "server restart") {
		t.Errorf("remedy should remind the user to restart; got %q", remedy)
	}
	if strings.ContainsRune(remedy, '…') {
		t.Errorf("remedy must not contain the … placeholder; got %q", remedy)
	}
	if !strings.Contains(remedy, "config get security.allowedDomains") {
		t.Errorf("remedy should append to existing domains via config get; got %q", remedy)
	}

	// A hostless target cannot be allowlisted, so it gets no remedy rather than
	// one that cannot work.
	if got := idpiAllowlistRemedy("about:blank"); got != "" {
		t.Errorf("expected no remedy for a hostless url; got %q", got)
	}
	if _, ok := idpiRefusedURLDetails("about:blank")["remedy"]; ok {
		t.Error("a hostless refused URL must not carry a remedy")
	}
}

func TestIDPIScannerHint(t *testing.T) {
	hint := idpiScannerHint()
	if !strings.Contains(hint, "strictMode") {
		t.Errorf("scanner hint should point at strictMode; got %q", hint)
	}
	if !strings.Contains(hint, "server restart") {
		t.Errorf("scanner hint should remind the user to restart; got %q", hint)
	}
}

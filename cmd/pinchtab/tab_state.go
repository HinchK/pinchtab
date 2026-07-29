package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pinchtab/pinchtab/internal/cli/apiclient"
	"github.com/spf13/cobra"
)

// tabStateStore owns the local "current tab" state for anonymous CLI calls:
// state-file path selection, freshness/TTL tracking, on-disk reads/writes, and
// the authenticated existence probe against a running server. Centralizing it
// here keeps these filesystem/network side effects out of command registration
// (cmd_cli_register.go), which now only wires the --tab flag onto commands and
// no longer has to thread tab-cache policy through the CLI wiring.
type tabStateStore struct{}

// defaultTabState is the process-wide tab-state service used by CLI wiring and
// the thin package-level helpers below.
var defaultTabState = tabStateStore{}

// useLocal reports whether anonymous local tab-state should be used. Identified
// callers (PINCHTAB_SESSION, --agent-id, or PINCHTAB_AGENT_ID) defer to the
// server-side scoped current-tab store instead.
func (tabStateStore) useLocal() bool {
	if strings.TrimSpace(os.Getenv("PINCHTAB_SESSION")) != "" {
		return false
	}
	return resolveCLIAgentID() == ""
}

// path is scoped to the server the tab belongs to. A single global file made two
// ordinary servers — a daemon on the default port plus a project server — fight
// over one tab ID, so a nav against one sent its tab to the other and every read
// 404ed. The slug is derived from the resolved base URL, so switching ports (or
// restarting on a different one) reads a different file rather than a wrong tab.
func (s tabStateStore) path() string {
	return s.dir() + "/current-tab-" + s.serverSlug()
}

func (tabStateStore) dir() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return dir + "/pinchtab"
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home + "/.local/state/pinchtab"
	}
	return "/tmp/pinchtab"
}

// serverSlug turns the resolved base URL into a filename-safe identity. It stays
// readable (127.0.0.1-9930) so an operator can tell which server a state file
// belongs to without decoding a hash.
func (tabStateStore) serverSlug() string {
	base, _ := resolveTabStateEndpoint()
	slug := strings.TrimPrefix(strings.TrimPrefix(base, "http://"), "https://")
	slug = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-':
			return r
		default:
			return '-'
		}
	}, strings.Trim(slug, "/"))
	if slug == "" {
		return "default"
	}
	return slug
}

func (s tabStateStore) read() string {
	data, err := os.ReadFile(s.path())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (s tabStateStore) write(tabID string) {
	if tabID == "" || !s.useLocal() {
		return
	}
	path := s.path()
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	_ = os.WriteFile(path, []byte(tabID+"\n"), 0644)
}

func (s tabStateStore) clearIfCurrent(tabID string) {
	if tabID == "" || !s.useLocal() || s.read() != tabID {
		return
	}
	_ = os.Remove(s.path())
}

// resolveArg returns the explicit positional tab argument when present, else the
// locally cached current tab (for anonymous single-agent workflows).
func (s tabStateStore) resolveArg(args []string) string {
	if len(args) > 0 && args[0] != "" {
		return args[0]
	}
	if !s.useLocal() {
		return ""
	}
	return s.read()
}

// applyDefaultFlag fills an unset --tab from the cached current tab, probing the
// server for existence only when the cache is stale (older than tabProbeTTL).
func (s tabStateStore) applyDefaultFlag(cmd *cobra.Command) {
	if cmd == nil || !s.useLocal() {
		return
	}
	flag := cmd.Flags().Lookup("tab")
	if flag == nil || flag.Changed || flag.Value.String() != "" {
		return
	}
	tabID := s.read()
	if tabID == "" {
		return
	}
	// Probed on every command rather than behind a freshness window. The window
	// existed to save a request, but a server restart invalidates every tab id
	// immediately, so trusting the cache for a fixed period is exactly the case that
	// broke: the first read after a restart still sent the dead id. Measured against
	// a live local server the probe costs ~1ms, and the expensive case — nothing
	// listening — never issues a request at all. Nothing refreshes a trust window
	// any more, so no inconclusive answer can keep a stale entry alive.
	if s.probe(tabID) == tabProbeGone {
		_ = os.Remove(s.path())
		return
	}
	_ = cmd.Flags().Set("tab", tabID)
	flag.Changed = false
}

// tabProbeResult distinguishes the three answers a probe can give. The old
// boolean collapsed "the server says this tab is gone" with "I could not tell",
// and the caller then treated both as a successful validation.
type tabProbeResult int

const (
	// tabProbeInconclusive: nothing listening, a transport error, or a status that
	// does not answer the question (401 without the right credential, 5xx).
	tabProbeInconclusive tabProbeResult = iota
	tabProbeAlive
	tabProbeGone
)

// probe asks the server the cached tab belongs to whether it still exists.
func (tabStateStore) probe(tabID string) tabProbeResult {
	base, token := resolveTabStateEndpoint()

	// Fast path: if the port isn't listening, skip the HTTP probe entirely. This
	// avoids a 2s timeout on every CLI command when the server is down — and it is
	// inconclusive, not a validation: the server may auto-start later.
	if !portIsListening(base) {
		return tabProbeInconclusive
	}

	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequest("GET", base+"/tabs/"+tabID+"/title", nil)
	if err != nil {
		return tabProbeInconclusive
	}
	req.Header.Set("X-PinchTab-Source", "client")
	if token != "" {
		if strings.HasPrefix(token, "ses_") {
			req.Header.Set("Authorization", "Session "+token)
		} else {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return tabProbeInconclusive
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return tabProbeGone
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return tabProbeAlive
	default:
		// A 401 means the probe lacked the credential, not that the tab is gone.
		return tabProbeInconclusive
	}
}

// A tab-not-found 404 that names an ID the user never typed is unactionable on its
// own — the ID came from this cache, so this is the only layer that can say so.
// Clearing it here is the remedy rather than instructions to delete a file: the
// retry the message asks for is what makes it true.
func init() { apiclient.ErrorRemedy = defaultTabState.staleTabRemedy }

func (s tabStateStore) staleTabRemedy(statusCode int, body []byte) string {
	if statusCode != http.StatusNotFound || !s.useLocal() {
		return ""
	}
	cached := s.read()
	if cached == "" || !bytes.Contains(body, []byte(cached)) {
		return ""
	}
	_ = os.Remove(s.path())
	base, _ := resolveTabStateEndpoint()
	return fmt.Sprintf("that tab id is this CLI's cached current tab for %s, not something you asked for; the cache is now cleared, so retry the command (or run `pinchtab nav <url>` to open a fresh tab)", base)
}

// The package-level helpers below preserve the existing CLI/test API by
// delegating to defaultTabState; command files call these directly while the
// service owns the filesystem/network logic.

func resolveTabArg(args []string) string { return defaultTabState.resolveArg(args) }

func defaultTabFlagFromState(cmd *cobra.Command) { defaultTabState.applyDefaultFlag(cmd) }

func useLocalTabStateFile() bool { return defaultTabState.useLocal() }

func tabStateFile() string { return defaultTabState.path() }

// WriteTabStateFile persists the current tab for anonymous local workflows.
func WriteTabStateFile(tabID string) { defaultTabState.write(tabID) }

// ClearTabStateFileIfCurrent removes the cached tab only if it matches tabID.
func ClearTabStateFileIfCurrent(tabID string) { defaultTabState.clearIfCurrent(tabID) }

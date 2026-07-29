package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/cli/apiclient"
	"github.com/spf13/cobra"
)

// tabStateHarness points the tab-state store at a temp state dir and at a stub
// server, resolving the endpoint the way the CLI does rather than through env
// vars — the divergence between those two resolutions is the defect under test.
type tabStateHarness struct {
	base      string
	token     string
	statusFor func(tabID string) int
	requests  []*http.Request
}

func newTabStateHarness(t *testing.T) *tabStateHarness {
	t.Helper()
	h := &tabStateHarness{token: "cfg-token", statusFor: func(string) int { return http.StatusOK }}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.requests = append(h.requests, r)
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/tabs/"), "/title")
		if got := r.Header.Get("Authorization"); got != "Bearer "+h.token {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		status := h.statusFor(id)
		w.WriteHeader(status)
		if status == http.StatusNotFound {
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "tab " + id + " not found"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"title": "ok"})
	}))
	t.Cleanup(srv.Close)
	h.base = srv.URL

	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("PINCHTAB_SESSION", "")
	t.Setenv("PINCHTAB_AGENT_ID", "")
	t.Setenv("PINCHTAB_SERVER", "")
	t.Setenv("PINCHTAB_TOKEN", "")
	oldAgent := cliAgentID
	cliAgentID = ""
	oldResolve := resolveTabStateEndpoint
	resolveTabStateEndpoint = func() (string, string) { return h.base, h.token }
	t.Cleanup(func() {
		cliAgentID = oldAgent
		resolveTabStateEndpoint = oldResolve
	})
	return h
}

func tabFlagCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "read"}
	cmd.Flags().String("tab", "", "Tab ID")
	return cmd
}

// The card's failing case: nav writes the cache, the server restarts, and the very
// next read must not send the dead id. The write is fresh, which is what the old
// freshness window trusted — so this is the case a TTL-gated probe cannot catch.
func TestCachedTabClearedAfterRestartWithNoInterveningNav(t *testing.T) {
	h := newTabStateHarness(t)
	WriteTabStateFile("TAB-BEFORE-RESTART")

	// The restart: the id the cache holds is unknown to the server now.
	h.statusFor = func(string) int { return http.StatusNotFound }

	cmd := tabFlagCommand()
	defaultTabFlagFromState(cmd)

	if got, _ := cmd.Flags().GetString("tab"); got != "" {
		t.Fatalf("--tab = %q after a restart, want empty so the command targets the server's current tab", got)
	}
	if _, err := os.Stat(tabStateFile()); !os.IsNotExist(err) {
		t.Fatalf("stale state file survived: %v", err)
	}
	if len(h.requests) == 0 {
		t.Fatal("no probe was issued; a freshly written cache must still be checked")
	}
}

// The probe has to reach the same server with the same credential as the command.
// With a config-file port and token and no env vars, the old probe queried 9867
// unauthenticated: nothing listening, "assume valid", stale forever.
func TestProbeUsesTheCommandsBaseURLAndToken(t *testing.T) {
	h := newTabStateHarness(t)
	WriteTabStateFile("LIVE-TAB")
	h.statusFor = func(id string) int {
		if id == "LIVE-TAB" {
			return http.StatusOK
		}
		return http.StatusNotFound
	}

	cmd := tabFlagCommand()
	defaultTabFlagFromState(cmd)

	if got, _ := cmd.Flags().GetString("tab"); got != "LIVE-TAB" {
		t.Fatalf("--tab = %q, want the live cached tab", got)
	}
	if len(h.requests) != 1 {
		t.Fatalf("probe issued %d requests, want 1", len(h.requests))
	}
	req := h.requests[0]
	if req.Host != strings.TrimPrefix(h.base, "http://") {
		t.Errorf("probe went to %q, want the command's server %q", req.Host, h.base)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer "+h.token {
		t.Errorf("probe Authorization = %q, want the config-file token", got)
	}
	if got := req.URL.Path; got != "/tabs/LIVE-TAB/title" {
		t.Errorf("probe path = %q", got)
	}
}

// Two servers, two caches. A single global file made an ordinary setup (a daemon
// plus a project server) send one server's tab id to the other.
func TestCurrentTabIsScopedPerServer(t *testing.T) {
	h := newTabStateHarness(t)

	WriteTabStateFile("TAB-ON-A")
	pathA := tabStateFile()

	h.base = "http://127.0.0.1:19999"
	WriteTabStateFile("TAB-ON-B")
	pathB := tabStateFile()

	if pathA == pathB {
		t.Fatalf("both servers share one state file: %s", pathA)
	}
	if got := defaultTabState.read(); got != "TAB-ON-B" {
		t.Fatalf("second server reads %q, want its own TAB-ON-B", got)
	}

	// The first server's cached tab must be untouched by the second server's nav.
	data, err := os.ReadFile(pathA)
	if err != nil {
		t.Fatalf("first server's state file: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "TAB-ON-A" {
		t.Fatalf("first server's cached tab = %q, want TAB-ON-A", got)
	}
}

// An inconclusive probe must not buy the stale entry any trust: the next command
// probes again, and when that probe can finally answer, the entry is cleared. The
// old code touched the file on every assume-valid return, so back-to-back commands
// renewed the window forever.
func TestInconclusiveProbeDoesNotProtectAStaleEntry(t *testing.T) {
	h := newTabStateHarness(t)
	WriteTabStateFile("STALE-TAB")

	// Round one: the probe cannot tell — wrong credential, so 401, not 404.
	h.token = "a-different-token"
	first := tabFlagCommand()
	defaultTabFlagFromState(first)
	if got, _ := first.Flags().GetString("tab"); got != "STALE-TAB" {
		t.Fatalf("--tab = %q, want the cached tab used when the probe is inconclusive", got)
	}
	if _, err := os.Stat(tabStateFile()); err != nil {
		t.Fatalf("an inconclusive probe must not delete the cache: %v", err)
	}

	// Round two, immediately after: the credential works and the server answers 404.
	h.token = "cfg-token"
	h.statusFor = func(string) int { return http.StatusNotFound }
	second := tabFlagCommand()
	defaultTabFlagFromState(second)

	if got, _ := second.Flags().GetString("tab"); got != "" {
		t.Fatalf("--tab = %q on the second command; the first probe's inconclusive answer suppressed the second", got)
	}
	if _, err := os.Stat(tabStateFile()); !os.IsNotExist(err) {
		t.Fatal("stale entry survived a definitive 404 on the very next command")
	}
}

// A 404 naming an id the user never typed is unactionable without saying where the
// id came from. The remedy also clears the cache, so the retry it asks for works.
func TestStaleTabErrorCarriesTheCachedTabRemedy(t *testing.T) {
	newTabStateHarness(t)
	WriteTabStateFile("CACHED-TAB")

	body := []byte(`{"code":"tab_not_found","error":"tab CACHED-TAB not found"}`)
	remedy := apiclient.ErrorRemedy(http.StatusNotFound, body)

	if remedy == "" {
		t.Fatal("no remedy for a 404 naming the cached tab")
	}
	for _, want := range []string{"cached current tab", "retry", "pinchtab nav"} {
		if !strings.Contains(remedy, want) {
			t.Errorf("remedy %q does not mention %q", remedy, want)
		}
	}
	if _, err := os.Stat(tabStateFile()); !os.IsNotExist(err) {
		t.Error("the remedy claims the cache is cleared but the file survived")
	}

	// A 404 about someone else's tab is not this cache's business.
	WriteTabStateFile("CACHED-TAB")
	if got := apiclient.ErrorRemedy(http.StatusNotFound, []byte(`{"error":"tab SOMEONE-ELSE not found"}`)); got != "" {
		t.Errorf("remedy %q offered for a tab the cache never held", got)
	}
	if _, err := os.Stat(tabStateFile()); err != nil {
		t.Errorf("unrelated 404 cleared the cache: %v", err)
	}
}

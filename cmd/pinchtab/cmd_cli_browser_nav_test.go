package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestTabHandoffFamilyRefusesLocallyOnAnEmptyTabID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a request was issued for an empty tab id: %s %s — the refusal must happen before the wire, or the mux 404s /tabs//<verb> and the CLI misdiagnoses the server as outdated", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("PINCHTAB_SERVER", srv.URL)
	t.Setenv("PINCHTAB_SESSION", "")
	t.Setenv("PINCHTAB_AGENT_ID", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	orig := resolveTabStateEndpoint
	resolveTabStateEndpoint = func() (string, string) { return srv.URL, "" }
	t.Cleanup(func() { resolveTabStateEndpoint = orig })

	for name, build := range map[string]func() *cobra.Command{
		"handoff":        newTabHandoffCmd,
		"resume":         newTabResumeCmd,
		"handoff-status": newTabHandoffStatusCmd,
	} {
		t.Run(name, func(t *testing.T) {
			cmd := build()
			cmd.SetArgs([]string{})
			err := cmd.Execute()
			if !errors.Is(err, errNoCurrentTab) {
				t.Fatalf("err = %v, want the local no-current-tab refusal", err)
			}
			for _, lever := range []string{"tab id", "pinchtab nav"} {
				if !strings.Contains(err.Error(), lever) {
					t.Errorf("refusal %q does not name the %q lever", err, lever)
				}
			}
		})
	}
}

func TestMustResolveTabArgPassesAnExplicitOrCachedID(t *testing.T) {
	t.Setenv("PINCHTAB_SESSION", "")
	t.Setenv("PINCHTAB_AGENT_ID", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	orig := resolveTabStateEndpoint
	resolveTabStateEndpoint = func() (string, string) { return "http://127.0.0.1:19999", "" }
	t.Cleanup(func() { resolveTabStateEndpoint = orig })

	if id, err := mustResolveTabArg([]string{"TAB123"}); err != nil || id != "TAB123" {
		t.Fatalf("explicit id: got %q, %v", id, err)
	}

	defaultTabState.write("CACHED42")
	if id, err := mustResolveTabArg(nil); err != nil || id != "CACHED42" {
		t.Fatalf("cached id: got %q, %v — the refusal must not swallow the cache fallback", id, err)
	}
}

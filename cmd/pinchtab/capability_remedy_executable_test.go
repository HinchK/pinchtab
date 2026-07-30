package main

import (
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/config"
	"github.com/pinchtab/pinchtab/internal/httpx"
	"github.com/pinchtab/pinchtab/internal/routes"
)

// capabilitySettings are the config paths a capability refusal can name, derived
// from the route catalogue. Clipboard is appended because it gates endpoints
// without a catalogue entry, so nothing else would carry it here.
func capabilitySettings(t *testing.T) []string {
	t.Helper()

	settings := []string{"security.allowClipboard"}
	for capability := range routes.CapabilityEndpoints() {
		meta, ok := routes.Meta(capability)
		if !ok {
			t.Errorf("capability %q gates endpoints but has no metadata", capability)
			continue
		}
		settings = append(settings, meta.Setting)
	}
	if len(settings) < 2 {
		t.Fatal("no capability settings found; this test would prove nothing")
	}
	return settings
}

// A remedy an agent cannot execute is not a remedy. This resolves each command in
// the refusal against the REAL command tree — the same rootCmd the binary runs —
// rather than eyeballing the string, because the failure mode being closed is a
// remedy that reads plausibly and dead-ends when run.
func TestCapabilityRemedyCommandsResolveInTheCLI(t *testing.T) {
	for _, setting := range capabilitySettings(t) {
		remedy, _ := httpx.DisabledEndpointDetails(setting)["remedy"].(string)

		commands := strings.Split(remedy, "&&")
		if len(commands) != 2 {
			t.Fatalf("remedy for %s is not two shell-joined commands: %q", setting, remedy)
		}

		for _, command := range commands {
			fields := strings.Fields(strings.TrimSpace(command))
			if len(fields) == 0 || fields[0] != "pinchtab" {
				t.Errorf("remedy command %q does not invoke pinchtab", command)
				continue
			}
			args := fields[1:]

			found, rest, err := rootCmd.Find(args)
			if err != nil {
				t.Errorf("remedy command %q does not resolve: %v", command, err)
				continue
			}
			if !found.Runnable() {
				t.Errorf("remedy command %q resolves to %q, which is a command group and does nothing when run",
					command, found.CommandPath())
				continue
			}
			if found.Args != nil {
				if err := found.Args(found, rest); err != nil {
					t.Errorf("remedy command %q resolves to %q but its arguments %v are rejected: %v",
						command, found.CommandPath(), rest, err)
				}
			}
		}
	}
}

// The other half of executable: `config set <path> true` is only real if the
// config editor accepts that path. A setting present in the schema but missing
// from the editor's field table dead-ends every message that cites it.
func TestCapabilityRemedySettingsAreAcceptedByTheConfigEditor(t *testing.T) {
	for _, setting := range capabilitySettings(t) {
		fc := config.FileConfig{}
		if err := config.SetConfigValue(&fc, setting, "true"); err != nil {
			t.Errorf("the remedy tells the caller to run `pinchtab config set %s true`, which the editor rejects: %v", setting, err)
		}
	}
}

// The restart half must name a command that exists at that exact path. Finding it
// through the tree rather than asserting the literal means a renamed or moved
// verb reds here instead of shipping a remedy nobody can run.
func TestCapabilityRemedyRestartCommandExists(t *testing.T) {
	remedy, _ := httpx.DisabledEndpointDetails("security.allowCookies")["remedy"].(string)

	const restart = "pinchtab server restart"
	if !strings.Contains(remedy, restart) {
		t.Fatalf("remedy = %q, want it to name %q", remedy, restart)
	}
	found, _, err := rootCmd.Find([]string{"server", "restart"})
	if err != nil || found.CommandPath() != "pinchtab server restart" {
		t.Fatalf("`%s` does not resolve to itself (got %q, err %v); the remedy names a command that no longer exists",
			restart, found.CommandPath(), err)
	}
}

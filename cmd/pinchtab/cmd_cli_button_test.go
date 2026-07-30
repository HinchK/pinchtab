package main

import (
	"strings"
	"testing"

	bridgecdpops "github.com/pinchtab/pinchtab/internal/bridge/cdpops"
	"github.com/spf13/cobra"
)

// buttonCommands is derived from the tree rather than hand-listed: every command that
// registers --button must refuse an unknown one locally, so a fourth command gaining the flag
// is covered on arrival.
func buttonCommands(t *testing.T) []*cobra.Command {
	t.Helper()

	var found []*cobra.Command
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		if cmd.Flags().Lookup("button") != nil {
			found = append(found, cmd)
		}
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)

	if len(found) < 3 {
		t.Fatalf("found %d commands with --button, so this census is not walking the command tree", len(found))
	}
	return found
}

// The local refusal is a fast path for a typo — better than a server round trip — and it must
// not be the only guard, which the HTTP-side test covers.
func TestEveryButtonFlagRefusesAnUnknownNameLocally(t *testing.T) {
	for _, cmd := range buttonCommands(t) {
		path := cmd.CommandPath()
		if cmd.PreRunE == nil {
			t.Errorf("%s registers --button with no local check, so a typo costs a server round trip to discover", path)
			continue
		}

		if err := cmd.Flags().Set("button", "rihgt"); err != nil {
			t.Fatal(err)
		}
		err := cmd.PreRunE(cmd, nil)
		if err == nil {
			t.Errorf("%s accepted a misspelled button", path)
		} else if !strings.Contains(err.Error(), "rihgt") {
			t.Errorf("%s refusal = %v, want it to name what was sent", path, err)
		}

		for _, valid := range append(bridgecdpops.MouseButtons(), "RIGHT", " middle ") {
			if err := cmd.Flags().Set("button", valid); err != nil {
				t.Fatal(err)
			}
			if err := cmd.PreRunE(cmd, nil); err != nil {
				t.Errorf("%s refused %q: %v", path, valid, err)
			}
		}

		if err := cmd.Flags().Set("button", bridgecdpops.DefaultMouseButton); err != nil {
			t.Fatal(err)
		}
	}
}

// The help text and the default both come from the vocabulary owner, so the next button is
// not documented in one place and refused in another.
func TestEveryButtonFlagDerivesItsHelpFromTheVocabulary(t *testing.T) {
	for _, cmd := range buttonCommands(t) {
		flag := cmd.Flags().Lookup("button")
		if flag.DefValue != bridgecdpops.DefaultMouseButton {
			t.Errorf("%s --button default = %q, want the vocabulary's own default %q", cmd.CommandPath(), flag.DefValue, bridgecdpops.DefaultMouseButton)
		}
		for _, name := range bridgecdpops.MouseButtons() {
			if !strings.Contains(flag.Usage, name) {
				t.Errorf("%s --button help = %q, want it to name %q", cmd.CommandPath(), flag.Usage, name)
			}
		}
	}
}

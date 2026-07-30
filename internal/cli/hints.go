package cli

import (
	"fmt"
	"io"
)

// CommandHint is one "<command>  <comment>" row in a CLI hint group.
type CommandHint struct {
	Command string
	Comment string
}

// WriteCommandHints renders a heading followed by aligned command/comment rows.
// When styled, the heading/command/comment use the cli styles (ANSI); otherwise
// they are emitted plain. width is the command-column pad (the %-44s/%-64s the
// call sites used inline); padding counts ANSI bytes when styled, matching the
// previous inline behavior exactly.
func WriteCommandHints(out io.Writer, heading string, hints []CommandHint, width int, styled bool) {
	if styled {
		_, _ = fmt.Fprintln(out, StyleStdout(HeadingStyle, heading))
	} else {
		_, _ = fmt.Fprintln(out, heading)
	}
	for _, h := range hints {
		cmd, comment := h.Command, h.Comment
		if styled {
			cmd = StyleStdout(CommandStyle, cmd)
			comment = StyleStdout(MutedStyle, comment)
		}
		_, _ = fmt.Fprintf(out, "  %-*s %s\n", width, cmd, comment)
	}
}

// SessionCreateCommand is the one spelling of the create-a-session command, so
// every place that recommends it stays in lockstep.
const SessionCreateCommand = "export PINCHTAB_SESSION=$(pinchtab session create --agent-id <id>)"

// NoSessionHint is the single wording for "this caller has no agent session".
//
// It must hold in BOTH server states, which is why the enable-and-restart clause is
// conditional and comes last. It used to lead with "agent sessions must be enabled",
// which kept a reader on default config from dead-ending in the command's own
// "agent sessions are not enabled on this server" — but told every correctly
// configured user to change config that was already right and restart a server that
// needed no restart. This is the most-printed string in the product, so it says the
// applicable half first and keeps the prerequisite as the fallback: a reader on
// either server still reaches a working outcome by following it top to bottom.
//
// A THIRD state is still wrong here and this wording does not fix it: on a bridge
// there are no agent sessions at all, so the otherwise-clause cannot help. Fixing
// that needs the capability in the health payload (nothing there reports whether
// agent sessions are enabled today) so the caller can pick a branch; do not read
// this constant as true on a bridge.
const NoSessionHint = "this tab is shared — no agent session is set. If agent sessions are enabled on this server, create one with: " +
	SessionCreateCommand + "; otherwise set sessions.agent.enabled = true in config.json and restart first."

// NextStepsRunningHints is the "Next steps" group shown when the server is up;
// shared by the root banner and `pinchtab health` so the two stay in lockstep.
var NextStepsRunningHints = []CommandHint{
	{SessionCreateCommand, "# start a dedicated session"},
	{"pinchtab nav <url>", "# navigate the current tab (headless by default)"},
	{"pinchtab snap", "# inspect interactive elements"},
}

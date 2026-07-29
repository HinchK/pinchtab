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
// It names the server-side prerequisite before the command, because the command
// exits 1 with "agent sessions are not enabled on this server" on default
// config — so a user following the hint top to bottom cannot dead-end.
const NoSessionHint = "this tab is shared — no agent session is set. Agent sessions must be enabled on the server " +
	"(sessions.agent.enabled = true in config.json, then restart); once they are, create one with: " + SessionCreateCommand

// NextStepsRunningHints is the "Next steps" group shown when the server is up;
// shared by the root banner and `pinchtab health` so the two stay in lockstep.
var NextStepsRunningHints = []CommandHint{
	{SessionCreateCommand, "# start a dedicated session"},
	{"pinchtab nav <url>", "# navigate the current tab (headless by default)"},
	{"pinchtab snap", "# inspect interactive elements"},
}

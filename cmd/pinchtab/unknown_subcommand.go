package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// unknownSubcommandExitCode is the one exit code for a typo'd verb anywhere in the CLI.
// It is 1 because that is what cobra's own error path already returns for a top-level
// unknown command, and every other reported error in this CLI exits 1 as well; daemon's
// hand-rolled 2 is what changed to match, not the other way round.
const unknownSubcommandExitCode = 1

// installUnknownSubcommandGuard makes an unrecognised subcommand an error on every group,
// which is what cobra does NOT do: its default argument check only rejects an unknown
// command on the ROOT, so `pinchtab cache clera` fell through to the group — printing help,
// or in the case of a runnable group such as `config` running the group's own action and
// swallowing the arguments — and exited 0. A script cannot tell that from the real verb, so
// `set -e` does not trip and the state it believed it reset was never reset.
//
// The guard is derived from the command tree rather than a list of group names, because a
// hand-written list is exactly how eleven groups came to be missing it. A group that
// declares its own Args is left alone: that declaration is how a parent whose operand is
// data rather than a verb opts out — `tab [id]` focuses a tab, `network <filter>` takes a
// pattern — and it is also the record of that decision.
//
// It must run after every command is registered, so it belongs at Execute rather than in an
// init: registration is spread across many init functions and their order is not a contract.
func installUnknownSubcommandGuard(cmd *cobra.Command) {
	for _, sub := range cmd.Commands() {
		installUnknownSubcommandGuard(sub)
	}
	if !cmd.HasSubCommands() || !cmd.HasParent() || cmd.Args != nil {
		return
	}
	cmd.Args = rejectUnknownSubcommand
	if !cmd.Runnable() {
		// A group with no action of its own never reaches its argument check: cobra returns
		// "print the help" for an unrunnable command BEFORE validating arguments, which is why
		// setting Args alone left `pinchtab cache clera` at exit 0. Printing the help is what
		// the command already did for no arguments, so this keeps that and nothing more.
		cmd.RunE = printGroupHelp
	}
}

func printGroupHelp(cmd *cobra.Command, _ []string) error {
	return cmd.Help()
}

// rejectUnknownSubcommand names the valid verbs in the error itself rather than relying on
// the usage template, which this CLI shortens to a single "run --help" line.
func rejectUnknownSubcommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	message := fmt.Sprintf("unknown command %q for %q\nValid subcommands: %s",
		args[0], cmd.CommandPath(), strings.Join(subcommandNames(cmd), ", "))
	return newCommandExitError(unknownSubcommandExitCode, fmt.Errorf("%s", message))
}

func subcommandNames(cmd *cobra.Command) []string {
	var names []string
	for _, sub := range cmd.Commands() {
		if sub.IsAvailableCommand() {
			names = append(names, sub.Name())
		}
	}
	sort.Strings(names)
	return names
}

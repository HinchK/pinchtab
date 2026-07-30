package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// unknownSubcommandExitCode is 1 because that is what cobra's own error path already returns
// for a top-level unknown command; daemon's hand-rolled 2 is what changed to match.
const unknownSubcommandExitCode = 1

// installUnknownSubcommandGuard runs at Execute rather than in an init because registration
// is spread across many init functions and their order is not a contract. A group declaring
// its own Args opts out, which is how a parent whose first argument is data rather than a verb
// says so.
func installUnknownSubcommandGuard(cmd *cobra.Command) {
	for _, sub := range cmd.Commands() {
		installUnknownSubcommandGuard(sub)
	}
	if !cmd.HasSubCommands() || !cmd.HasParent() || cmd.Args != nil {
		return
	}
	cmd.Args = rejectUnknownSubcommand
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
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

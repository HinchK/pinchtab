package main

import (
	"fmt"
	"os"

	"github.com/pinchtab/pinchtab/internal/browsers/chrome"
	"github.com/pinchtab/pinchtab/internal/browsers/runtimekit"
	"github.com/pinchtab/pinchtab/internal/cli"
	"github.com/pinchtab/pinchtab/internal/config"
	"github.com/pinchtab/pinchtab/internal/daemon"
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon [action]",
	Short: "Manage the background service",
	Long:  "Start, stop, install, or check the status of the PinchTab background service.",
	Run: func(cmd *cobra.Command, args []string) {
		jsonOut, _ := cmd.Flags().GetBool("json")
		sub := ""
		if len(args) > 0 {
			sub = args[0]
		}
		handleDaemonCommand(sub, jsonOut)
	},
}

func init() {
	daemonCmd.GroupID = "primary"
	daemonCmd.Flags().Bool("json", false, "Print daemon status as JSON (status only, no actions)")
	rootCmd.AddCommand(daemonCmd)
}

var daemonCurrentManager = daemon.CurrentManager

func handleDaemonCommand(subcommand string, jsonOut bool) {
	if code := dispatchDaemonCommand(subcommand, jsonOut); code != 0 {
		os.Exit(code)
	}
}

func dispatchDaemonCommand(subcommand string, jsonOut bool) int {
	if isDaemonStatusSubcommand(subcommand) {
		if jsonOut {
			printDaemonStatusJSON()
			return 0
		}
		printDaemonOverview()
		return 0
	}

	policy, declared := daemonNotInstalledPolicies[subcommand]
	if !declared {
		return printDaemonUsage(subcommand)
	}
	if handled, code := applyDaemonNotInstalledPolicy(subcommand, policy); handled {
		return code
	}

	manager, err := daemonCurrentManager()
	if err != nil {
		fmt.Fprintln(os.Stderr, cli.StyleStderr(cli.ErrorStyle, err.Error()))
		return 1
	}

	switch subcommand {
	case "install":
		handleDaemonInstall(manager)
	case "start":
		printDaemonManagerResult(manager.Start())
	case "restart":
		printDaemonManagerResult(manager.Restart())
	case "stop":
		printDaemonManagerResult(manager.Stop())
	case "uninstall":
		handleDaemonUninstall(manager)
	default:
		return printDaemonUsage(subcommand)
	}
	return 0
}

func printDaemonUsage(subcommand string) int {
	fmt.Fprintln(os.Stderr, cli.StyleStderr(cli.ErrorStyle, fmt.Sprintf("unknown daemon command: %s", subcommand)))
	fmt.Fprintln(os.Stderr, cli.StyleStderr(cli.MutedStyle, "Usage: pinchtab daemon <status|install|start|restart|stop|uninstall>"))
	return unknownSubcommandExitCode
}

func isDaemonStatusSubcommand(subcommand string) bool {
	switch subcommand {
	case "", "status", "help", "--help", "-h":
		return true
	}
	return false
}

// daemonNotInstalledPolicy is what one lifecycle verb does when the service is not
// installed. Every verb DECLARES its answer: the previous guard skipped the check for
// anything that was not start or restart, so stop and uninstall bypassed it by
// construction and affirmed work they had not done. A verb missing from the table below
// is not silently unguarded — it does not dispatch at all.
type daemonNotInstalledPolicy int

const (
	// daemonRefuse: exit 1 with the install remedy, touching no manager.
	daemonRefuse daemonNotInstalledPolicy = iota
	// daemonNoOp: exit 0 saying plainly that there was nothing to do.
	daemonNoOp
	// daemonProceed: the not-installed state is this verb's normal input.
	daemonProceed
)

// The contract is idempotent for stop and uninstall, and that is not a fresh choice —
// it is the one the manager layer already made. launchdManager.Stop swallows the
// not-loaded error and Uninstall swallows os.ErrNotExist on removing the plist, both
// deliberately, so refusing here would contradict the layer below and break the
// provisioning chain `daemon stop && daemon install && daemon start`, which never
// reaches install if stop exits 1.
var daemonNotInstalledPolicies = map[string]daemonNotInstalledPolicy{
	"install":   daemonProceed,
	"start":     daemonRefuse,
	"restart":   daemonRefuse,
	"stop":      daemonNoOp,
	"uninstall": daemonNoOp,
}

// daemonNothingToDo is the honest wording for a no-op verb. It states the observed fact
// and what did not happen, where "  [ok] Pinchtab daemon stopped." asserted an action
// that was never performed — which an operator, or a provisioning script reading exit 0
// plus that sentence, takes as confirmation.
var daemonNothingToDo = map[string]string{
	"stop":      "Background service is not installed; nothing to stop.",
	"uninstall": "Background service is not installed; nothing to uninstall.",
}

// applyDaemonNotInstalledPolicy asks the installation question ONCE and lets the verb's
// policy decide the wording and the exit code. It reports handled=true when the verb is
// finished and must not reach a manager.
func applyDaemonNotInstalledPolicy(subcommand string, policy daemonNotInstalledPolicy) (bool, int) {
	if policy == daemonProceed {
		return false, 0
	}

	installed, err := daemonInstallationStatus()
	if err != nil {
		if policy == daemonRefuse {
			fmt.Fprintln(os.Stderr, cli.StyleStderr(cli.ErrorStyle,
				fmt.Sprintf("cannot determine whether the background service is installed; refusing to %s: %v", subcommand, err)))
			return true, 1
		}
		// Could-not-read is not absence, and must never be rendered as one: "nothing to
		// stop" built from an unknown is a false statement. The managers already tolerate
		// a service that is not there, so the honest move is to attempt the operation and
		// report whatever they say.
		return false, 0
	}
	if installed {
		return false, 0
	}

	if policy == daemonRefuse {
		fmt.Fprintln(os.Stderr, cli.StyleStderr(cli.ErrorStyle,
			"background service is not installed; install it first with: pinchtab daemon install"))
		return true, 1
	}
	fmt.Println(cli.StyleStdout(cli.SuccessStyle, "  [ok] ") + daemonNothingToDo[subcommand])
	return true, 0
}

func handleDaemonInstall(manager daemon.Manager) {
	configPath, fileCfg, _, err := daemon.EnsureConfig(false)
	if err != nil {
		fmt.Fprintln(os.Stderr, cli.StyleStderr(cli.ErrorStyle, fmt.Sprintf("daemon install failed: %v", err)))
		os.Exit(1)
	}
	if config.NeedsWizard(fileCfg) {
		isNew := config.IsFirstRun(fileCfg)
		runSecurityWizard(fileCfg, configPath, isNew)
	}
	if err := manager.Preflight(); err != nil {
		fmt.Fprintln(os.Stderr, cli.StyleStderr(cli.ErrorStyle, fmt.Sprintf("daemon install unavailable: %v", err)))
		os.Exit(1)
	}
	message, err := manager.Install(configPath)
	if err != nil {
		printDaemonActionError(manager, fmt.Sprintf("daemon install failed: %v", err))
	}
	fmt.Println(cli.StyleStdout(cli.SuccessStyle, "  [ok] ") + message)
	warnPrimaryChromeMacOS(loadConfig())
	printDaemonFollowUp()
}

// warnPrimaryChromeMacOS surfaces the issue #583 collision at install time:
// on macOS, auto-launching the user's daily Google Chrome for headless
// automation can stop their normal Chrome from opening a window.
func warnPrimaryChromeMacOS(cfg *config.RuntimeConfig) {
	effective := runtimekit.ResolveEffectiveBrowser(cfg)
	if effective.ID != config.BrowserChrome || !chrome.IsPrimaryChromeBinaryMacOS(effective.Binary) {
		return
	}
	fmt.Fprintln(os.Stderr, cli.StyleStderr(cli.WarningStyle,
		"  [warn] Automation will use your primary Google Chrome on macOS."))
	fmt.Fprintln(os.Stderr, cli.StyleStderr(cli.MutedStyle,
		"         Launching it headless can stop your normal Chrome from opening (issue #583)."))
	fmt.Fprintln(os.Stderr, cli.StyleStderr(cli.MutedStyle,
		"         Install Google Chrome for Testing or Chromium, or set browser.binary in config"))
	fmt.Fprintln(os.Stderr, cli.StyleStderr(cli.MutedStyle,
		"         to a dedicated automation browser."))
}

func handleDaemonUninstall(manager daemon.Manager) {
	message, err := manager.Uninstall()
	if err != nil {
		printDaemonActionError(manager, err.Error())
	}
	fmt.Println(cli.StyleStdout(cli.SuccessStyle, "  [ok] ") + message)
}

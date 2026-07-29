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

	if err := requireDaemonInstalled(subcommand); err != nil {
		fmt.Fprintln(os.Stderr, cli.StyleStderr(cli.ErrorStyle, err.Error()))
		return 1
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
		fmt.Fprintln(os.Stderr, cli.StyleStderr(cli.ErrorStyle, fmt.Sprintf("unknown daemon command: %s", subcommand)))
		fmt.Fprintln(os.Stderr, cli.StyleStderr(cli.MutedStyle, "Usage: pinchtab daemon <status|install|start|restart|stop|uninstall>"))
		return 2
	}
	return 0
}

func isDaemonStatusSubcommand(subcommand string) bool {
	switch subcommand {
	case "", "status", "help", "--help", "-h":
		return true
	}
	return false
}

func requireDaemonInstalled(subcommand string) error {
	if subcommand != "start" && subcommand != "restart" {
		return nil
	}
	installed, err := daemonInstallationStatus()
	if err != nil {
		return fmt.Errorf("cannot determine whether the background service is installed; refusing to %s: %w", subcommand, err)
	}
	if installed {
		return nil
	}
	return fmt.Errorf("background service is not installed; install it first with: pinchtab daemon install")
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

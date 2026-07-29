package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/pinchtab/pinchtab/internal/cli"
	"github.com/pinchtab/pinchtab/internal/config"
	"github.com/pinchtab/pinchtab/internal/config/workflow"
	"github.com/pinchtab/pinchtab/internal/safelog"
	"github.com/pinchtab/pinchtab/internal/server"
	"github.com/spf13/cobra"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start server",
	Run: func(cmd *cobra.Command, args []string) {
		maybeRunWizard()

		cfg := loadConfig()
		backgroundMarker, _ := cmd.Flags().GetString("background-child")
		cfg.BackgroundMarker = backgroundMarker

		bind, _ := cmd.Flags().GetString("bind")
		port, _ := cmd.Flags().GetString("port")
		applyServerAddressFlags(cfg, bind, port)

		yolo, _ := cmd.Flags().GetBool("yolo")
		if yolo {
			fc, _, err := config.LoadFileConfig()
			if err != nil {
				fmt.Fprintln(os.Stderr, cli.StyleStderr(cli.ErrorStyle, fmt.Sprintf("--yolo: load config: %v", err)))
				os.Exit(1)
			}
			if _, err := workflow.BuildGuardsDownConfig(fc); err != nil {
				fmt.Fprintln(os.Stderr, cli.StyleStderr(cli.ErrorStyle, fmt.Sprintf("--yolo: %v", err)))
				os.Exit(1)
			}
			config.ApplyFileConfigToRuntime(cfg, fc)
			fmt.Fprintln(os.Stderr, cli.StyleStderr(cli.WarningStyle, "YOLO mode: guards down for this run only (config file unchanged)"))
		}

		headed, _ := cmd.Flags().GetBool("headed")
		if headed {
			cfg.Headless = false
			cfg.HeadlessSet = true
		}
		exts, _ := cmd.Flags().GetStringArray("extension")
		if len(exts) > 0 {
			cfg.ExtensionPaths = append(cfg.ExtensionPaths, exts...)
		}
		verbose, _ := cmd.Flags().GetBool("verbose")
		cfg.VerboseBanner = verbose
		logLevel, _ := cmd.Flags().GetString("log-level")
		resolveLogLevel(cfg, logLevel, verbose)

		browserName, _ := cmd.Flags().GetString("browser")
		if browserName != "" {
			browser, err := config.ParseBrowser(browserName, cfg.BrowsersAvailable)
			if err != nil {
				fmt.Fprintln(os.Stderr, cli.StyleStderr(cli.ErrorStyle, err.Error()))
				os.Exit(1)
			}
			cfg.DefaultBrowser = browser
		}

		if background, _ := cmd.Flags().GetBool("background"); background {
			if err := runServerBackground(cfg, serverBackgroundOptions{
				Yolo:       yolo,
				Headed:     headed,
				Verbose:    verbose,
				LogLevel:   logLevel,
				Extensions: append([]string(nil), exts...),
				Browser:    browserName,
				Bind:       bind,
				Port:       port,
			}); err != nil {
				fmt.Fprintln(os.Stderr, cli.StyleStderr(cli.ErrorStyle, err.Error()))
				os.Exit(1)
			}
			return
		}
		server.RunDashboard(cfg, version)
	},
}

// applyServerAddressFlags applies the --bind/--port overrides, mirroring the
// bridge's precedence: a non-empty flag wins over config, an omitted one leaves
// the configured value in place.
func applyServerAddressFlags(cfg *config.RuntimeConfig, bind, port string) {
	if v := strings.TrimSpace(bind); v != "" {
		cfg.Bind = v
	}
	if v := strings.TrimSpace(port); v != "" {
		cfg.Port = v
	}
}

// resolveLogLevel settles a run's threshold from the only inputs that carry it, in
// precedence order: --log-level, then server.logLevel from the config file, then
// -v, then the default. Every command that logs calls this rather than assigning
// cfg.LogLevel itself — an unconditional assignment erased server.logLevel on
// every flagless run, which is every daemon-installed and auto-started server.
func resolveLogLevel(cfg *config.RuntimeConfig, logLevel string, verbose bool) {
	if v := strings.TrimSpace(logLevel); v != "" {
		cfg.LogLevel = v
	}
	applyLogLevel(cfg, verbose)
}

func applyLogLevel(cfg *config.RuntimeConfig, verbose bool) {
	if strings.TrimSpace(cfg.LogLevel) != "" {
		level, err := safelog.ParseLevel(cfg.LogLevel)
		if err != nil {
			fmt.Fprintln(os.Stderr, cli.StyleStderr(cli.ErrorStyle, "pinchtab: "+err.Error()))
			os.Exit(1)
		}
		safelog.SetLevel(level)
		return
	}
	if verbose {
		safelog.SetLevel(slog.LevelDebug)
		return
	}
	safelog.SetLevel(safelog.DefaultLevel)
}

func init() {
	serverCmd.GroupID = "primary"
	serverCmd.Flags().String("bind", "", "Bind address for the HTTP server (overrides config server.bind)")
	serverCmd.Flags().String("port", "", "Port for the HTTP server (overrides config server.port)")
	serverCmd.Flags().StringArrayP("extension", "e", nil, "Load browser extension (repeatable)")
	serverCmd.Flags().BoolP("headed", "H", false, "Start default instance in headed mode")
	serverCmd.Flags().BoolP("yolo", "y", false, "Apply guards down preset (enables evaluate, macro, download, cookies)")
	serverCmd.Flags().BoolP("verbose", "v", false, "Show the full startup banner and log at debug level")
	serverCmd.Flags().String("log-level", "", "Minimum log level: debug, info (default), warn or error")
	serverCmd.Flags().String("browser", "", "Browser to use: chrome, cloak, or ghost-chrome (overrides config)")
	serverCmd.Flags().BoolP("background", "b", false, "Spawn the server detached and return JSON with pid/url/token")
	serverCmd.Flags().String("background-child", "", "Internal marker for background server ownership")
	_ = serverCmd.Flags().MarkHidden("background-child")
	serverCmd.AddCommand(serverStopCmd)
	serverCmd.AddCommand(serverRestartCmd)
	rootCmd.AddCommand(serverCmd)
}

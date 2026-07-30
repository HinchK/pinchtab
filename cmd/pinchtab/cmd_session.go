package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/pinchtab/pinchtab/internal/cli"
	"github.com/pinchtab/pinchtab/internal/cli/apiclient"
	"github.com/pinchtab/pinchtab/internal/cli/output"
	"github.com/spf13/cobra"
)

type sessionCreateResult struct {
	ID     string `json:"id"`
	Token  string `json:"sessionToken"`
	Status string `json:"status"`
}

// printSessionCreated splits the two values by stream on purpose. The token is the only
// thing on stdout, because the documented usage captures it with $(...). The id goes to
// stderr as a hint: it is the handle `session revoke` takes, and withholding it is why
// revoking your own session used to need a list-and-correlate detour through
// `session list`.
func printSessionCreated(result sessionCreateResult) {
	fmt.Println(result.Token)
	if result.ID != "" {
		output.Hint("session id " + result.ID + " — revoke takes the id: pinchtab session revoke " + result.ID)
	}
}

func init() {
	sessionCmd := &cobra.Command{
		Use:     "session",
		Short:   "Agent session management",
		GroupID: "primary",
	}

	infoCmd := &cobra.Command{
		Use:   "info",
		Short: "Show current agent session details",
		Run: func(cmd *cobra.Command, args []string) {
			sessionToken := os.Getenv("PINCHTAB_SESSION")
			if sessionToken == "" {
				fmt.Fprintln(os.Stderr, "Error: no session set")
				output.Hint(cli.NoSessionHint)
				os.Exit(1)
			}
			runCLI(func(rt cliRuntime) {
				apiclient.DoGet(rt.client, rt.base, rt.token, "/sessions/me", nil)
			})
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all agent sessions",
		Run: func(cmd *cobra.Command, args []string) {
			runCLI(func(rt cliRuntime) {
				apiclient.DoGet(rt.client, rt.base, rt.token, "/sessions", nil)
			})
		},
	}

	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new agent session",
		Run: func(cmd *cobra.Command, args []string) {
			agentID, _ := cmd.Flags().GetString("agent-id")
			label, _ := cmd.Flags().GetString("label")
			jsonOutput, _ := cmd.Flags().GetBool("json")
			if agentID == "" {
				fmt.Fprintln(os.Stderr, "Error: --agent-id is required")
				os.Exit(1)
			}
			body := map[string]any{"agentId": agentID}
			if label != "" {
				body["label"] = label
			}
			// Auto-start the control plane first: the documented "create a session
			// before any browser command" order otherwise fails cold on a fresh
			// machine (only browser commands start the server).
			runCLIEnsuringServerNoBrowser("session create", func(rt cliRuntime) {
				if jsonOutput {
					apiclient.DoPost(rt.client, rt.base, rt.token, "/sessions", body)
					return
				}
				statusCode, rawBody, _ := apiclient.DoPostQuietWithStatus(rt.client, rt.base, rt.token, "/sessions", body)
				if statusCode == 404 {
					fmt.Fprintln(os.Stderr, "Error: agent sessions are not enabled on this server")
					output.Hint("enable with: sessions.agent.enabled = true in config.json, then restart the server")
					os.Exit(1)
				}
				if statusCode >= 400 {
					apiclient.ExitWithAPIError(statusCode, rawBody)
				}
				var result sessionCreateResult
				if err := json.Unmarshal(rawBody, &result); err != nil {
					fmt.Fprintf(os.Stderr, "Error: failed to parse session response\n")
					os.Exit(1)
				}
				if result.Status != "active" {
					fmt.Fprintf(os.Stderr, "Error: session is %s\n", result.Status)
					output.Hint("create a new session: pinchtab session create --agent-id <id>")
					os.Exit(1)
				}
				printSessionCreated(result)
			})
		},
	}
	createCmd.Flags().String("agent-id", "", "Agent ID to associate with the session (required)")
	createCmd.Flags().String("label", "", "Optional human-readable label")
	addJSONFlag(createCmd)

	revokeCmd := &cobra.Command{
		Use:   "revoke <session-id>",
		Short: "Revoke an agent session",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			runCLI(func(rt cliRuntime) {
				apiclient.DoPost(rt.client, rt.base, rt.token, "/sessions/"+args[0]+"/revoke", nil)
			})
		},
	}

	sessionCmd.AddCommand(infoCmd, listCmd, createCmd, revokeCmd)
	rootCmd.AddCommand(sessionCmd)
}

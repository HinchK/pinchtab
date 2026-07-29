package actions

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/pinchtab/pinchtab/internal/cli"
	"github.com/pinchtab/pinchtab/internal/cli/apiclient"
	"github.com/pinchtab/pinchtab/internal/cli/output"
	"github.com/spf13/cobra"
)

// Back navigates the current (or specified) tab back in history.
func Back(client *http.Client, base, token string, cmd *cobra.Command) {
	historyNav(client, base, token, "back", cmd)
}

// Forward navigates the current (or specified) tab forward in history.
func Forward(client *http.Client, base, token string, cmd *cobra.Command) {
	historyNav(client, base, token, "forward", cmd)
}

// Reload reloads the current (or specified) tab.
func Reload(client *http.Client, base, token string, cmd *cobra.Command) {
	historyNav(client, base, token, "reload", cmd)
}

// historyNav is the one body behind back, forward and reload. The server answers
// all three from a single handler returning {"tabId","url"}, so the CLI reports
// the landed URL the same way for each rather than keeping three copies that
// drift — reload used to discard the response and print a bare OK.
func historyNav(client *http.Client, base, token, action string, cmd *cobra.Command) {
	tabID, _ := cmd.Flags().GetString("tab")
	path := "/" + action
	if tabID != "" {
		path = "/tabs/" + tabID + "/" + action
	}
	path = appendDismissBannersQuery(path, cmd)

	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		apiclient.DoPost(client, base, token, path, nil)
		return
	}

	result := apiclient.DoPostQuiet(client, base, token, path, nil)
	if landed := landedURL(result); landed != "" {
		output.Value(landed)
	} else {
		output.Success()
	}

	printPostActionOutput(client, base, token, tabID, cmd)
}

// printPostActionOutput runs the shared --snap / --snap-diff / --text tail.
func printPostActionOutput(client *http.Client, base, token, tabID string, cmd *cobra.Command) {
	snap, _ := cmd.Flags().GetBool("snap")
	snapDiff, _ := cmd.Flags().GetBool("snap-diff")
	if snap || snapDiff {
		fetchAndPrintSnapshot(client, base, token, tabID, snapDiff)
	}
	if text, _ := cmd.Flags().GetBool("text"); text {
		fetchAndPrintText(client, base, token, tabID)
	}
}

// landedURL is the URL the navigation actually ended on, which is not the
// requested one whenever a redirect, login wall or error page intervened. Empty
// when the response carries none, so callers can stay terse instead of printing
// a blank line.
func landedURL(result map[string]any) string {
	landed, _ := result["url"].(string)
	return landed
}

func Navigate(client *http.Client, base, token string, url string, cmd *cobra.Command) string {
	req := buildNavigateRequest(url, cmd)

	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		result := postNavigate(client, base, token, req, true)
		apiclient.SuggestNextAction("navigate", result)
		return tabIDFromNavigateResult(result)
	}

	result := postNavigate(client, base, token, req, false)
	resultTabID := tabIDFromNavigateResult(result)
	if resultTabID != "" {
		fmt.Println(resultTabID)
	}

	// The landed URL is the cheap signal that the page is not the one asked for,
	// but a second line would break `TAB=$(pinchtab nav URL)` — command
	// substitution captures every line. So it is printed only for a human at a
	// terminal, which is what --print-tab-id already promised.
	if !tabIDOnly(cmd) {
		if landed := landedURL(result); landed != "" {
			output.Value(landed)
		}
	}

	if !isIdentifiedCaller() {
		output.Hint(cli.NoSessionHint)
	}

	printPostActionOutput(client, base, token, resultTabID, cmd)

	return resultTabID
}

// tabIDOnly reports whether stdout must carry nothing but the tab ID: either
// --print-tab-id was passed, or stdout is not a terminal, which is the shape a
// capture or a pipeline has.
func tabIDOnly(cmd *cobra.Command) bool {
	if only, _ := cmd.Flags().GetBool("print-tab-id"); only {
		return true
	}
	return !stdoutIsTerminal()
}

// stdoutIsTerminal stands in for an environment a test cannot set, so it is a
// var; production reads its result on every non-JSON navigate.
var stdoutIsTerminal = func() bool {
	info, err := os.Stdout.Stat()
	return err == nil && (info.Mode()&os.ModeCharDevice) != 0
}

type navigateRequest struct {
	path               string
	body               map[string]any
	fallbackOnNotFound bool
}

func buildNavigateRequest(url string, cmd *cobra.Command) navigateRequest {
	body := map[string]any{"url": url}
	newTab, _ := cmd.Flags().GetBool("new-tab")
	if newTab {
		body["newTab"] = true
	}
	if v, _ := cmd.Flags().GetBool("block-images"); v {
		body["blockImages"] = true
	}
	if v, _ := cmd.Flags().GetBool("block-ads"); v {
		body["blockAds"] = true
	}
	if v, _ := cmd.Flags().GetBool("dismiss-banners"); v {
		body["dismissBanners"] = true
	}
	tabID, _ := cmd.Flags().GetString("tab")
	path := "/navigate"
	explicitTab := cmd.Flags().Changed("tab")
	fallbackOnNotFound := false
	// Don't use tab-specific path when creating a new tab. If the tab came from
	// the saved current-tab state file and no longer exists, retry through
	// /navigate so the server can create/select a current tab. Explicit --tab
	// remains strict and surfaces the 404.
	if tabID != "" && !newTab {
		path = "/tabs/" + tabID + "/navigate"
		fallbackOnNotFound = !explicitTab
	}

	return navigateRequest{
		path:               path,
		body:               body,
		fallbackOnNotFound: fallbackOnNotFound,
	}
}

func postNavigate(client *http.Client, base, token string, req navigateRequest, printResponse bool) map[string]any {
	statusCode, respBody, result := apiclient.DoPostQuietWithStatus(client, base, token, req.path, req.body)
	if statusCode == http.StatusNotFound && req.fallbackOnNotFound {
		statusCode, respBody, result = apiclient.DoPostQuietWithStatus(client, base, token, "/navigate", req.body)
	}
	if statusCode >= 400 {
		apiclient.ExitWithAPIError(statusCode, respBody)
	}
	if printResponse {
		return apiclient.PrintAndDecode(respBody)
	}
	return result
}

func isIdentifiedCaller() bool {
	return strings.TrimSpace(os.Getenv("PINCHTAB_SESSION")) != "" ||
		strings.TrimSpace(os.Getenv("PINCHTAB_AGENT_ID")) != ""
}

func tabIDFromNavigateResult(result map[string]any) string {
	if tid, ok := result["tabId"].(string); ok && tid != "" {
		return tid
	}
	return ""
}

// appendDismissBannersQuery appends ?dismissBanners=true (or &dismissBanners=true)
// to the given path when the cobra command's --dismiss-banners flag is set.
// Used by /back, /forward, /reload which don't carry a JSON body.
func appendDismissBannersQuery(path string, cmd *cobra.Command) string {
	v, _ := cmd.Flags().GetBool("dismiss-banners")
	if !v {
		return path
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "dismissBanners=true"
}

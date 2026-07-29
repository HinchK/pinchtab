package actions

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/cli"
	"github.com/spf13/cobra"
)

func newNavigateCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("new-tab", false, "")
	cmd.Flags().Bool("block-images", false, "")
	cmd.Flags().Bool("block-ads", false, "")
	cmd.Flags().Bool("dismiss-banners", false, "")
	cmd.Flags().String("tab", "", "")
	cmd.Flags().Bool("print-tab-id", false, "")
	return cmd
}

func newHistoryCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("tab", "", "")
	cmd.Flags().Bool("snap", false, "")
	cmd.Flags().Bool("snap-diff", false, "")
	cmd.Flags().Bool("text", false, "")
	cmd.Flags().Bool("dismiss-banners", false, "")
	cmd.Flags().Bool("json", false, "")
	return cmd
}

func TestNavigate(t *testing.T) {
	m := newMockServer()
	defer m.close()
	client := m.server.Client()

	cmd := newNavigateCmd()
	Navigate(client, m.base(), "", "https://pinchtab.com", cmd)
	if m.lastMethod != "POST" {
		t.Errorf("expected POST, got %s", m.lastMethod)
	}
	if m.lastPath != "/navigate" {
		t.Errorf("expected /navigate, got %s", m.lastPath)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(m.lastBody), &body)
	if body["url"] != "https://pinchtab.com" {
		t.Errorf("expected url=https://pinchtab.com, got %v", body["url"])
	}
}

func TestNavigateReusesImplicitTabWhenItExists(t *testing.T) {
	m := newMockServer()
	m.response = `{"tabId":"ABC123","status":"ok"}`
	defer m.close()
	client := m.server.Client()

	cmd := newNavigateCmd()
	cmd.Flags().Lookup("tab").DefValue = "ABC123"
	_ = cmd.Flags().Set("tab", "ABC123")
	cmd.Flags().Lookup("tab").Changed = false

	Navigate(client, m.base(), "", "https://pinchtab.com", cmd)

	if len(m.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(m.requests))
	}
	if m.requests[0].Path != "/tabs/ABC123/navigate" {
		t.Fatalf("navigate path = %q, want /tabs/ABC123/navigate", m.requests[0].Path)
	}
}

func TestNavigateFallsBackToNewTabForStaleImplicitTab(t *testing.T) {
	m := newMockServer()
	m.setResponse(http.MethodPost, "/tabs/STALE123/navigate", http.StatusNotFound, `{"error":"tab not found"}`)
	m.setResponse(http.MethodPost, "/navigate", http.StatusOK, `{"tabId":"NEW123","status":"ok"}`)
	defer m.close()
	client := m.server.Client()

	cmd := newNavigateCmd()
	cmd.Flags().Lookup("tab").DefValue = "STALE123"
	_ = cmd.Flags().Set("tab", "STALE123")
	cmd.Flags().Lookup("tab").Changed = false

	Navigate(client, m.base(), "", "https://pinchtab.com", cmd)

	if len(m.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(m.requests))
	}
	if m.requests[0].Path != "/tabs/STALE123/navigate" {
		t.Fatalf("first request path = %q, want /tabs/STALE123/navigate", m.requests[0].Path)
	}
	if m.requests[1].Path != "/navigate" {
		t.Fatalf("navigate path = %q, want /navigate", m.requests[1].Path)
	}
}

func TestBuildNavigateRequestDoesNotFallbackForExplicitTab(t *testing.T) {
	cmd := newNavigateCmd()
	_ = cmd.Flags().Set("tab", "EXPLICIT123")

	req := buildNavigateRequest("https://pinchtab.com", cmd)

	if req.path != "/tabs/EXPLICIT123/navigate" {
		t.Fatalf("path = %q, want /tabs/EXPLICIT123/navigate", req.path)
	}
	if req.fallbackOnNotFound {
		t.Fatal("explicit --tab should not fallback on 404")
	}
}

func TestNavigateWithAllFlags(t *testing.T) {
	m := newMockServer()
	defer m.close()
	client := m.server.Client()

	cmd := newNavigateCmd()
	_ = cmd.Flags().Set("new-tab", "true")
	_ = cmd.Flags().Set("block-images", "true")
	Navigate(client, m.base(), "", "https://pinchtab.com", cmd)
	var body map[string]any
	_ = json.Unmarshal([]byte(m.lastBody), &body)
	if body["newTab"] != true {
		t.Error("expected newTab=true")
	}
	if body["blockImages"] != true {
		t.Error("expected blockImages=true")
	}
}

func TestNavigateWithBlockAds(t *testing.T) {
	m := newMockServer()
	defer m.close()
	client := m.server.Client()

	cmd := newNavigateCmd()
	_ = cmd.Flags().Set("block-ads", "true")
	Navigate(client, m.base(), "", "https://pinchtab.com", cmd)
	var body map[string]any
	_ = json.Unmarshal([]byte(m.lastBody), &body)
	if body["blockAds"] != true {
		t.Error("expected blockAds=true")
	}
}

func TestNavigateDismissBanners(t *testing.T) {
	m := newMockServer()
	defer m.close()
	client := m.server.Client()

	cmd := newNavigateCmd()
	_ = cmd.Flags().Set("dismiss-banners", "true")
	Navigate(client, m.base(), "", "https://pinchtab.com", cmd)
	var body map[string]any
	_ = json.Unmarshal([]byte(m.lastBody), &body)
	if body["dismissBanners"] != true {
		t.Errorf("expected dismissBanners=true in body, got %v", body["dismissBanners"])
	}
}

func TestReloadDismissBannersAppendsQuery(t *testing.T) {
	m := newMockServer()
	defer m.close()
	client := m.server.Client()

	cmd := newHistoryCmd()
	_ = cmd.Flags().Set("dismiss-banners", "true")
	Reload(client, m.base(), "", cmd)
	if m.lastPath != "/reload" {
		t.Errorf("expected /reload path, got %q", m.lastPath)
	}
	if !strings.Contains(m.lastQuery, "dismissBanners=true") {
		t.Errorf("expected dismissBanners=true in query, got %q", m.lastQuery)
	}
}

func TestBackDismissBannersAppendsQuery(t *testing.T) {
	m := newMockServer()
	defer m.close()
	client := m.server.Client()

	cmd := newHistoryCmd()
	_ = cmd.Flags().Set("dismiss-banners", "true")
	Back(client, m.base(), "", cmd)
	if m.lastPath != "/back" {
		t.Errorf("expected /back path, got %q", m.lastPath)
	}
	if !strings.Contains(m.lastQuery, "dismissBanners=true") {
		t.Errorf("expected dismissBanners=true in query, got %q", m.lastQuery)
	}
}

func TestForwardDismissBannersAppendsQueryWithTab(t *testing.T) {
	m := newMockServer()
	defer m.close()
	client := m.server.Client()

	cmd := newHistoryCmd()
	_ = cmd.Flags().Set("tab", "TAB1")
	_ = cmd.Flags().Set("dismiss-banners", "true")
	Forward(client, m.base(), "", cmd)
	if m.lastPath != "/tabs/TAB1/forward" {
		t.Errorf("expected /tabs/TAB1/forward, got %q", m.lastPath)
	}
	if !strings.Contains(m.lastQuery, "dismissBanners=true") {
		t.Errorf("expected dismissBanners=true in query, got %q", m.lastQuery)
	}
}

func TestReloadWithoutDismissBannersOmitsQuery(t *testing.T) {
	m := newMockServer()
	defer m.close()
	client := m.server.Client()

	cmd := newHistoryCmd()
	Reload(client, m.base(), "", cmd)
	if m.lastQuery != "" {
		t.Errorf("expected empty query, got %q", m.lastQuery)
	}
}

func TestNavigatePrintTabID(t *testing.T) {
	m := newMockServer()
	m.response = `{"tabId":"ABC123","status":"ok"}`
	defer m.close()
	client := m.server.Client()

	cmd := newNavigateCmd()
	_ = cmd.Flags().Set("print-tab-id", "true")

	out := captureStdout(t, func() {
		Navigate(client, m.base(), "", "https://pinchtab.com", cmd)
	})
	got := strings.TrimSpace(out)
	if got != "ABC123" {
		t.Errorf("stdout = %q, want exactly the tab ID so $(pinchtab nav URL) stays usable", got)
	}
}

func TestNavigateNoSessionHintNamesPrerequisiteAndCommand(t *testing.T) {
	t.Setenv("PINCHTAB_SESSION", "")
	t.Setenv("PINCHTAB_AGENT_ID", "")

	m := newMockServer()
	m.response = `{"tabId":"ABC123","status":"ok"}`
	defer m.close()

	stderr := captureStderr(t, func() {
		captureStdout(t, func() {
			Navigate(m.server.Client(), m.base(), "", "https://pinchtab.com", newNavigateCmd())
		})
	})

	if !strings.Contains(stderr, cli.NoSessionHint) {
		t.Fatalf("hint = %q, want the shared cli.NoSessionHint", stderr)
	}
	// Following the hint top to bottom must not dead-end in "agent sessions are
	// not enabled on this server", so it has to carry the server-side
	// prerequisite as well as the command.
	if !strings.Contains(cli.NoSessionHint, "sessions.agent.enabled = true") {
		t.Errorf("hint does not name the server-side prerequisite: %q", cli.NoSessionHint)
	}
	if !strings.Contains(cli.NoSessionHint, cli.SessionCreateCommand) {
		t.Errorf("hint does not carry the create command: %q", cli.NoSessionHint)
	}

	// The hint decision reads nothing from the server: navigate stays one request.
	if len(m.requests) != 1 || m.requests[0].Path != "/navigate" {
		t.Fatalf("requests = %+v, want exactly the navigate call", m.requests)
	}
}

func TestNavigateIdentifiedCallerPrintsNoSessionHint(t *testing.T) {
	t.Setenv("PINCHTAB_SESSION", "ses_something")

	m := newMockServer()
	m.response = `{"tabId":"ABC123","status":"ok"}`
	defer m.close()

	stderr := captureStderr(t, func() {
		captureStdout(t, func() {
			Navigate(m.server.Client(), m.base(), "", "https://pinchtab.com", newNavigateCmd())
		})
	})

	if strings.Contains(stderr, cli.SessionCreateCommand) {
		t.Fatalf("stderr = %q, want no session hint for an identified caller", stderr)
	}
}

func stdoutTerminal(t *testing.T, isTerminal bool) {
	t.Helper()
	old := stdoutIsTerminal
	stdoutIsTerminal = func() bool { return isTerminal }
	t.Cleanup(func() { stdoutIsTerminal = old })
}

// The landed URL is the cheap signal that a redirect, login wall or error page
// intervened, and the server already returns it.
func TestNavigateReportsTheLandedURLAtATerminal(t *testing.T) {
	stdoutTerminal(t, true)
	m := newMockServer()
	m.response = `{"tabId":"ABC123","title":"Example Domain","url":"https://example.com/"}`
	defer m.close()

	out := captureStdout(t, func() {
		Navigate(m.server.Client(), m.base(), "", "https://httpbin.org/redirect-to?url=https://example.com/", newNavigateCmd())
	})

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("stdout = %q, want the tab ID and the landed URL", out)
	}
	if lines[0] != "ABC123" {
		t.Errorf("first line = %q, want the tab ID", lines[0])
	}
	if lines[1] != "https://example.com/" {
		t.Errorf("second line = %q, want the landed URL, not the requested one", lines[1])
	}
}

// TAB=$(pinchtab nav URL) captures every line, so a second line would break it.
// Both the explicit flag and a non-terminal stdout must stay single-line.
func TestNavigatePrintsOnlyTheTabIDWhenCaptured(t *testing.T) {
	for _, tc := range []struct {
		name     string
		terminal bool
		flag     bool
	}{
		{name: "stdout is not a character device", terminal: false},
		{name: "print-tab-id at a terminal", terminal: true, flag: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdoutTerminal(t, tc.terminal)

			m := newMockServer()
			m.response = `{"tabId":"ABC123","url":"https://example.com/"}`
			defer m.close()

			cmd := newNavigateCmd()
			if tc.flag {
				_ = cmd.Flags().Set("print-tab-id", "true")
			}
			out := captureStdout(t, func() {
				Navigate(m.server.Client(), m.base(), "", "https://example.com", cmd)
			})

			if got := strings.TrimSpace(out); got != "ABC123" {
				t.Errorf("stdout = %q, want exactly the tab ID so $(pinchtab nav URL) stays usable", got)
			}
		})
	}
}

// back, forward and reload share one server handler returning {"tabId","url"}, and
// none of them has a tab ID on stdout to protect — so unlike nav they report the
// landed URL through a pipe as well. The name says "through a pipe" because that
// asymmetry is the thing a later reader might "tidy" in either direction.
func TestHistoryNavigationPrintsTheLandedURLThroughAPipe(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*http.Client, string, string, *cobra.Command)
		path string
	}{
		{name: "back", run: Back, path: "/back"},
		{name: "forward", run: Forward, path: "/forward"},
		{name: "reload", run: Reload, path: "/reload"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newMockServer()
			m.response = `{"tabId":"ABC123","url":"https://example.com/landed"}`
			defer m.close()

			stdoutTerminal(t, false)

			out := captureStdout(t, func() {
				tc.run(m.server.Client(), m.base(), "", newHistoryCmd())
			})

			if m.lastPath != tc.path {
				t.Fatalf("path = %q, want %q", m.lastPath, tc.path)
			}
			if got := strings.TrimSpace(out); got != "https://example.com/landed" {
				t.Errorf("stdout = %q, want the landed URL even when stdout is not a terminal", got)
			}
		})
	}
}

// A response without a url must stay terse rather than print a blank line: OK
// for the history commands, the bare tab ID for nav.
func TestLandingReportDegradesWithoutAURL(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*http.Client, string, string, *cobra.Command)
	}{
		{name: "back", run: Back},
		{name: "forward", run: Forward},
		{name: "reload", run: Reload},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newMockServer()
			m.response = `{"tabId":"ABC123"}`
			defer m.close()

			out := captureStdout(t, func() {
				tc.run(m.server.Client(), m.base(), "", newHistoryCmd())
			})
			if got := strings.TrimSpace(out); got != "OK" {
				t.Errorf("stdout = %q, want the terse OK", got)
			}
			if strings.Contains(out, "\n\n") {
				t.Errorf("stdout = %q, want no blank line", out)
			}
		})
	}

	t.Run("navigate", func(t *testing.T) {
		stdoutTerminal(t, true)
		m := newMockServer()
		m.response = `{"tabId":"ABC123"}`
		defer m.close()

		out := captureStdout(t, func() {
			Navigate(m.server.Client(), m.base(), "", "https://example.com", newNavigateCmd())
		})
		if got := strings.TrimSpace(out); got != "ABC123" {
			t.Errorf("stdout = %q, want just the tab ID", got)
		}
		if strings.Contains(out, "\n\n") {
			t.Errorf("stdout = %q, want no blank line", out)
		}
	})
}

// --json is the machine contract: the raw response body for all four commands, with
// no landed-URL line added.
func TestJSONOutputIsTheRawResponseForAllFour(t *testing.T) {
	const response = `{"tabId":"ABC123","url":"https://example.com/landed"}`
	// DoPost pretty-prints the decoded body, so that is what --json must equal.
	const want = "{\n  \"tabId\": \"ABC123\",\n  \"url\": \"https://example.com/landed\"\n}"

	t.Run("navigate", func(t *testing.T) {
		stdoutTerminal(t, true)
		m := newMockServer()
		m.response = response
		defer m.close()

		cmd := newNavigateCmd()
		cmd.Flags().Bool("json", false, "")
		_ = cmd.Flags().Set("json", "true")
		out := captureStdout(t, func() {
			Navigate(m.server.Client(), m.base(), "", "https://example.com", cmd)
		})
		if got := strings.TrimSpace(out); got != want {
			t.Errorf("stdout = %q, want the response body alone %q", got, want)
		}
	})

	for _, tc := range []struct {
		name string
		run  func(*http.Client, string, string, *cobra.Command)
	}{
		{name: "back", run: Back},
		{name: "forward", run: Forward},
		{name: "reload", run: Reload},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdoutTerminal(t, true)
			m := newMockServer()
			m.response = response
			defer m.close()

			cmd := newHistoryCmd()
			_ = cmd.Flags().Set("json", "true")
			out := captureStdout(t, func() {
				tc.run(m.server.Client(), m.base(), "", cmd)
			})
			if got := strings.TrimSpace(out); got != want {
				t.Errorf("stdout = %q, want the response body alone %q", got, want)
			}
		})
	}
}

// nav gained --text; the shared tail must actually run it.
func TestNavigateTextFetchesPageText(t *testing.T) {
	stdoutTerminal(t, true)
	m := newMockServer()
	m.response = `{"tabId":"ABC123","url":"https://example.com/"}`
	m.responses["GET /tabs/ABC123/text"] = mockResponse{statusCode: 200, body: `{"text":"PAGE TEXT"}`}
	defer m.close()

	cmd := newNavigateCmd()
	cmd.Flags().Bool("snap", false, "")
	cmd.Flags().Bool("snap-diff", false, "")
	cmd.Flags().Bool("text", false, "")
	_ = cmd.Flags().Set("text", "true")

	out := captureStdout(t, func() {
		Navigate(m.server.Client(), m.base(), "", "https://example.com", cmd)
	})

	if !strings.Contains(out, "PAGE TEXT") {
		t.Errorf("stdout = %q, want the page text after navigation", out)
	}
}

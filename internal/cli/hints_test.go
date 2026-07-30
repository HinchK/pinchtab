package cli

import (
	"strings"
	"testing"
)

// The hint is state-blind by design — the CLI cannot know whether the server has agent
// sessions enabled without a new health field or a round trip per nav — so the wording
// has to be TRUE in both states rather than correct in one. These assertions are on the
// properties that make that so, not on the sentence, because the sentence will be
// reworded again and a full-string compare would only pin today's phrasing.
func TestNoSessionHintHoldsWhetherOrNotSessionsAreEnabled(t *testing.T) {
	hint := NoSessionHint

	// The enabled server is the state the previous wording got wrong: it asserted the
	// prerequisite as an unmet requirement, so a correctly configured user was told to
	// change config that was already right and restart a server that needed no restart.
	for _, imperative := range []string{
		"must be enabled",
		"Agent sessions must be",
		"then restart)",
	} {
		if strings.Contains(hint, imperative) {
			t.Errorf("hint asserts %q as an unmet requirement, which is false on a server that already has sessions enabled: %q", imperative, hint)
		}
	}

	// The disabled server keeps the no-dead-end property: the prerequisite is still
	// named, so a reader following the hint top to bottom does not land in the command's
	// own "agent sessions are not enabled on this server" with nowhere to go.
	for _, needed := range []string{"sessions.agent.enabled = true", "restart", SessionCreateCommand} {
		if !strings.Contains(hint, needed) {
			t.Errorf("hint no longer carries %q; a reader on default config dead-ends without it: %q", needed, hint)
		}
	}

	// Order carries the meaning. The clause that applies to a working server comes
	// first, and the config change is reachable only through a conditional — that is
	// what stops it reading as an instruction to everyone.
	command := strings.Index(hint, SessionCreateCommand)
	fallback := strings.Index(hint, "sessions.agent.enabled = true")
	if command < 0 || fallback < 0 || command > fallback {
		t.Errorf("the create command must come before the enable-and-restart clause, so the applicable half is read first: %q", hint)
	}
	if !strings.Contains(hint, "If agent sessions are enabled") {
		t.Errorf("the create command is not conditional, so it reads as available on every server: %q", hint)
	}
	if !strings.Contains(hint, "otherwise") {
		t.Errorf("the enable-and-restart clause is not conditional, so it reads as an instruction to users who need nothing: %q", hint)
	}
}

// One constant, rendered by both call sites. The temptation once a hint has two halves is
// to specialise it per caller, which is how the two states drift apart again.
func TestNoSessionHintIsOneLineSoBothCallSitesRenderItWhole(t *testing.T) {
	if strings.Contains(NoSessionHint, "\n") {
		t.Errorf("hint spans lines; output.Hint prefixes a single HINT: line, so a newline breaks both call sites' formatting: %q", NoSessionHint)
	}
	if strings.TrimSpace(NoSessionHint) != NoSessionHint {
		t.Errorf("hint has surrounding whitespace, which shows up after HINT:: %q", NoSessionHint)
	}
}

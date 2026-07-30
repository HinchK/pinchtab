package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readSkillFile(t *testing.T, parts ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{"..", "..", "skills", "pinchtab"}, parts...)...)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s, so this guard would cover nothing: %v", path, err)
	}
	return string(raw)
}

// documentsNamespace reports whether the CLI reference gives a namespace its own
// section. Headings carry the command's argument form ("### `pinchtab nav <url>`"), so
// the name is matched with either a closing backtick or a following space — a mention
// inside another command's prose does not count.
func documentsNamespace(reference, name string) bool {
	return strings.Contains(reference, "### `pinchtab "+name+"`") ||
		strings.Contains(reference, "### `pinchtab "+name+" ")
}

// The defect this guard is derived from is a CROSS-SURFACE asymmetry, not incompleteness:
// an agent reading the skill could discover cookie setting over MCP and over raw HTTP,
// but not the CLI verb built for it — and for reading was sent to `state`, the command
// whose help no longer claims cookies as its job. The CLI is the token-cheap surface the
// skill steers agents toward, so a namespace present on the other two surfaces and absent
// here sends the agent to curl.
//
// Deliberately NOT "every namespace must be documented": measured against the tree, the
// CLI reference is a curated subset (roughly a third of the leaf verbs have no section),
// so that rule would assert a contract this repo does not have and would red for a dozen
// commands no card was filed about. Cross-surface parity is the narrower claim that is
// actually true and actually the defect.
//
// knownCrossSurfaceGaps is checked in BOTH directions, so it cannot go quiet: a NEW gap
// reds, and an entry whose namespace has since been documented reds as stale. Every entry
// carries what a reader needs to close it.
func TestNoNamespaceIsDocumentedOnAnotherSurfaceButMissingFromTheCLIReference(t *testing.T) {
	reference := readSkillFile(t, "references", "commands.md")
	mcp := readSkillFile(t, "references", "mcp.md")
	api := readSkillFile(t, "references", "api.md")

	knownCrossSurfaceGaps := map[string]string{
		"keyboard": "key-sequence verbs; documented on the MCP surface only — same class as the cookies gap, unfixed",
		"dialog":   "JS-dialog handling; documented on the MCP surface only — same class as the cookies gap, unfixed",
		"state":    "SKILL.md carries the state rules and the sensitivity warning inline, so the namespace is reachable there; a commands.md section is still owed",
	}

	namespaces := 0
	for _, cmd := range browserRootCommands() {
		if len(cmd.Commands()) == 0 {
			continue // a leaf verb is its own name; a namespace hides verbs an agent cannot guess
		}
		name := cmd.Name()
		namespaces++
		if documentsNamespace(reference, name) {
			if reason, stale := knownCrossSurfaceGaps[name]; stale {
				t.Errorf("`pinchtab %s` IS documented in the CLI reference now, but is still recorded as a known gap (%q); drop the entry so the guard keeps meaning what it says", name, reason)
			}
			continue
		}
		onAnotherSurface := strings.Contains(mcp, "pinchtab_"+name) || strings.Contains(api, "/"+name)
		if !onAnotherSurface {
			continue
		}
		if reason, ok := knownCrossSurfaceGaps[name]; ok {
			if reason == "" {
				t.Errorf("`pinchtab %s` is recorded as a known gap with no reason; every entry is a decision", name)
			}
			continue
		}
		t.Errorf("`pinchtab %s` is documented on the MCP or HTTP surface but has no `### `+\"`\"+`pinchtab %s`+\"`\"+` section in skills/pinchtab/references/commands.md, so an agent can discover the operation everywhere except on the surface the skill steers it toward. Add the section in the file's house style, or record it in knownCrossSurfaceGaps with the reason",
			name, name)
	}
	if namespaces < 8 {
		t.Fatalf("only %d namespaces reached this guard; the command list is not being read and it would pass vacuously", namespaces)
	}
}

// The routing half of the same defect: the skill sent an agent to `state` to read a
// cookie, which is precisely the misfiling the cookies namespace removed from the CLI
// help. Both surfaces must name the command that actually reads one.
func TestTheSkillRoutesCookieReadsAtTheCookiesCommand(t *testing.T) {
	for _, parts := range [][]string{{"SKILL.md"}, {"references", "commands.md"}} {
		name := filepath.Join(parts...)
		text := readSkillFile(t, parts...)
		if !strings.Contains(text, "pinchtab cookies get") {
			t.Errorf("%s never names `pinchtab cookies get`, so an agent reading it cannot find the command that reads a cookie", name)
		}
	}
}

// The browser-wide blast radius of `cookies clear` is pinned on the CLI side, and a doc
// that omits it teaches an agent to reach for a command that will log the user out of
// every site. The reference must not contradict the help it mirrors.
func TestTheCookiesReferenceStatesTheBrowserWideClearEffect(t *testing.T) {
	reference := readSkillFile(t, "references", "commands.md")
	// storage is pinned DIRECTLY rather than by the cross-surface rule above, and the
	// reason is measured: it has no MCP tool and no /storage HTTP route, so nothing on
	// another surface documents it and the derived rule cannot reach it. Removing its
	// section leaves that guard green — this assertion is what notices.
	for _, name := range []string{"cookies", "storage"} {
		if !documentsNamespace(reference, name) {
			t.Fatalf("the %s section is gone, so this guard is pinning nothing", name)
		}
	}
	for _, want := range []string{"all origins", "cannot be scoped"} {
		if !strings.Contains(reference, want) {
			t.Errorf("the CLI reference never says %q about `cookies clear`; its own --help does, and the doc must not read as narrower than the command is", want)
		}
	}
}

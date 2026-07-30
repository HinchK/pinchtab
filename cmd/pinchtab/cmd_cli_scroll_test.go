package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	browseractions "github.com/pinchtab/pinchtab/internal/cli/actions"
)

func resetScrollFlags(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		for _, name := range []string{"dy", "dx"} {
			flag := scrollCmd.Flags().Lookup(name)
			if flag == nil {
				continue
			}
			_ = flag.Value.Set("0")
			flag.Changed = false
		}
	})
}

// A second positional used to be accepted and dropped, which is how a scroll ran on the
// WRONG tab and still reported OK: `scroll -- -300 --tab <id>` put --tab and its value in
// args[1:], and MinimumNArgs(1) was happy. Refusing the count is the whole guard — it needs
// no special case for --tab.
func TestScrollRefusesArgumentsItCannotHonour(t *testing.T) {
	resetScrollFlags(t)

	for _, tc := range []struct {
		name    string
		args    []string
		flags   map[string]string
		wantErr string
	}{
		{
			name:    "the swallowed tab",
			args:    []string{"-300", "--tab", "zzz-not-a-tab"},
			wantErr: "at most 1 positional",
		},
		{
			name:    "any stray second positional",
			args:    []string{"800", "junk"},
			wantErr: "at most 1 positional",
		},
		{
			name:    "nothing to scroll by",
			args:    nil,
			wantErr: "--dy",
		},
		{
			name:    "two spellings of one argument",
			args:    []string{"800"},
			flags:   map[string]string{"dy": "-300"},
			wantErr: "not both",
		},
		{
			name:  "a positional alone",
			args:  []string{"800"},
			flags: nil,
		},
		{
			name:  "a selector alone",
			args:  []string{"e12"},
			flags: nil,
		},
		{
			name:  "the pixel flag alone",
			args:  nil,
			flags: map[string]string{"dy": "-300"},
		},
		{
			name:  "the horizontal flag alone",
			args:  nil,
			flags: map[string]string{"dx": "-120"},
		},
	} {
		for name, value := range tc.flags {
			if err := scrollCmd.Flags().Set(name, value); err != nil {
				t.Fatal(err)
			}
		}

		err := scrollCmd.Args(scrollCmd, tc.args)

		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("%s: scroll %v rejected with %v, want accepted", tc.name, tc.args, err)
		case tc.wantErr != "" && err == nil:
			t.Errorf("%s: scroll %v was accepted; anything it cannot honour must be refused rather than dropped", tc.name, tc.args)
		case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
			t.Errorf("%s: error = %v, want it to mention %q", tc.name, err, tc.wantErr)
		}

		for name := range tc.flags {
			flag := scrollCmd.Flags().Lookup(name)
			_ = flag.Value.Set("0")
			flag.Changed = false
		}
	}
}

// The negative count is only reachable through a flag, so the flags have to exist: a help
// text promising --dy over a command that never registered it is the same defect one layer up.
func TestScrollRegistersThePixelFlags(t *testing.T) {
	for _, name := range []string{"dy", "dx"} {
		flag := scrollCmd.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("scroll has no --%s, so a negative pixel count has no spelling that parses", name)
		}
		if flag.Value.Type() != "int" {
			t.Errorf("--%s is %s, want int: a signed count must be accepted as a flag VALUE", name, flag.Value.Type())
		}
	}
}

// directionsEnumeratedAfter extracts the keyword list a line presents after marker, so the
// prose can keep its own separators and order while the SET is what gets compared.
func directionsEnumeratedAfter(text, marker string) ([]string, bool) {
	for _, line := range strings.Split(text, "\n") {
		idx := strings.Index(line, marker)
		if idx < 0 {
			continue
		}
		list := line[idx+len(marker):]
		if cut := strings.Index(list, "("); cut >= 0 {
			list = list[:cut]
		}
		var found []string
		for _, token := range strings.FieldsFunc(list, func(r rune) bool {
			return r < 'a' || r > 'z'
		}) {
			if len(token) > 1 {
				found = append(found, token)
			}
		}
		sort.Strings(found)
		return found, true
	}
	return nil, false
}

// Every prose enumeration of the direction keywords is asserted against the code's own
// vocabulary, in BOTH directions: the agent-facing reference had invented "top" and
// "bottom", which fall through to CSS selectors and match nothing, while omitting "left"
// and "right", which work. A guard pinning one known-bad example would not have caught
// either half — only comparing the whole set does.
func TestDocumentedScrollDirectionsAreExactlyTheSupportedOnes(t *testing.T) {
	want := browseractions.ScrollDirectionKeywords()

	commands, err := os.ReadFile(filepath.Join("..", "..", "skills", "pinchtab", "references", "commands.md"))
	if err != nil {
		t.Fatalf("cannot read the agent-facing reference, so this guard would not cover it: %v", err)
	}

	for _, site := range []struct {
		name   string
		text   string
		marker string
	}{
		{name: "scroll --help", text: scrollCmd.Long, marker: "Direction keyword:"},
		{name: "skills/pinchtab/references/commands.md", text: string(commands), marker: "named direction:"},
	} {
		got, ok := directionsEnumeratedAfter(site.text, site.marker)
		if !ok {
			t.Errorf("%s no longer enumerates the directions after %q, so drift there is now unguarded", site.name, site.marker)
			continue
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s documents directions %v, want exactly %v — a keyword it invents falls through to a CSS selector and matches nothing, and one it omits is invisible to the reader", site.name, got, want)
		}
	}
}

// scroll -300 was an example in this help and in the agent-facing reference, and it never
// parsed — cobra reads the leading minus as shorthand flags. Both sites are checked here
// because a doc that teaches an unparseable command is how this reached a user.
func TestNoDocumentedScrollExampleUsesABareNegativePositional(t *testing.T) {
	docs := map[string]string{"scroll --help": scrollCmd.Long}

	for _, path := range []string{
		filepath.Join("..", "..", "skills", "pinchtab", "references", "commands.md"),
		filepath.Join("..", "..", "docs", "reference", "scroll.md"),
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("cannot read %s, so this guard would not cover the doc site it names: %v", path, err)
		}
		docs[filepath.ToSlash(path)] = string(raw)
	}

	for site, text := range docs {
		for _, line := range strings.Split(text, "\n") {
			fields := strings.Fields(line)
			for i, field := range fields {
				if field != "scroll" || i+1 >= len(fields) {
					continue
				}
				next := fields[i+1]
				if len(next) > 1 && next[0] == '-' && next[1] >= '0' && next[1] <= '9' {
					t.Errorf("%s teaches %q, which cobra reads as shorthand flags and refuses; a negative count is --dy <n>", site, strings.TrimSpace(line))
				}
			}
		}
	}
}

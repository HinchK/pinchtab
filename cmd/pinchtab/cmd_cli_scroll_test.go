package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

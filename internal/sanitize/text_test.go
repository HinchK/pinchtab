package sanitize

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCleanErrorRedactsAbsolutePaths(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "short unix path",
			input: "/var/log",
			want:  "[path]",
		},
		{
			name:  "quoted unix path",
			input: `error at "/Users/test/file.txt" failed`,
			want:  `error at "[path]" failed`,
		},
		{
			name:  "mixed unix and windows paths",
			input: `copy /var/log to C:\Users\test\file.txt`,
			want:  `copy [path] to [path]`,
		},
		{
			name:  "colon before unix path",
			input: `error:/Users/test/file.txt`,
			want:  `error:[path]`,
		},
		{
			name:  "colon before windows path",
			input: `error:C:\Users\test\file.txt`,
			want:  `error:[path]`,
		},
		{
			name:  "path-like substring inside word is preserved",
			input: `description/Users/guide`,
			want:  `description/Users/guide`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CleanError(tt.input, 1024); got != tt.want {
				t.Fatalf("CleanError(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPrefixUTF8BytesCutsOnRuneBoundaryWithoutMarker(t *testing.T) {
	const s = "héllo"

	tests := []struct {
		name     string
		maxBytes int
		want     string
	}{
		{name: "whole string fits", maxBytes: len(s), want: s},
		{name: "budget beyond the string", maxBytes: len(s) + 10, want: s},
		{name: "boundary before a two-byte rune", maxBytes: 1, want: "h"},
		{name: "budget lands inside a two-byte rune", maxBytes: 2, want: "h"},
		{name: "budget lands after a two-byte rune", maxBytes: 3, want: "hé"},
		{name: "zero budget", maxBytes: 0, want: ""},
		{name: "negative budget", maxBytes: -1, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PrefixUTF8Bytes(s, tt.maxBytes)
			if got != tt.want {
				t.Fatalf("PrefixUTF8Bytes(%q, %d) = %q, want %q", s, tt.maxBytes, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("PrefixUTF8Bytes(%q, %d) is not valid UTF-8", s, tt.maxBytes)
			}
			if strings.Contains(got, TruncationSuffix) {
				t.Fatalf("PrefixUTF8Bytes appended a truncation marker: %q", got)
			}
		})
	}
}

package chrome_test

import (
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/browsers/chrome"
)

// The Windows list used to be two hardcoded C:\Program Files paths. Chrome's
// installer puts the browser under %LOCALAPPDATA% whenever it runs without
// administrator rights — the common case on managed machines — and neither that
// nor the dedicated automation builds were probed.
func TestWindowsCommonPathsCoverPerUserAndAutomationInstalls(t *testing.T) {
	t.Setenv("ProgramFiles", `D:\Progs`)
	t.Setenv("LOCALAPPDATA", `D:\Users\alice\AppData\Local`)

	paths := chrome.CommonPaths("windows")
	if len(paths) == 0 {
		t.Fatal("windows CommonPaths empty")
	}

	for _, want := range []string{
		`D:\Users\alice\AppData\Local\Google\Chrome\Application\chrome.exe`,
		`D:\Progs\Google\Chrome\Application\chrome.exe`,
		`Google\Chrome for Testing\Application\chrome.exe`,
		`Chromium\Application\chrome.exe`,
	} {
		if !containsSubstring(paths, want) {
			t.Errorf("windows CommonPaths missing %q; got %v", want, paths)
		}
	}

	// Stable Chrome stays ahead of the pre-release channels.
	stable := indexOfSubstring(paths, `Google\Chrome\Application\chrome.exe`)
	canary := indexOfSubstring(paths, `Google\Chrome SxS\Application\chrome.exe`)
	if stable < 0 || canary < 0 || stable > canary {
		t.Errorf("stable Chrome should precede Canary; stable=%d canary=%d in %v", stable, canary, paths)
	}
}

// CommonPaths takes goos as a parameter, so it must emit Windows-shaped paths
// even when the test runs on another OS.
func TestWindowsCommonPathsUseBackslashesOnAnyHost(t *testing.T) {
	for _, p := range chrome.CommonPaths("windows") {
		if strings.Contains(p, "/") {
			t.Fatalf("windows path contains a forward slash: %q", p)
		}
	}
}

func containsSubstring(haystack []string, needle string) bool {
	return indexOfSubstring(haystack, needle) >= 0
}

func indexOfSubstring(haystack []string, needle string) int {
	for i, h := range haystack {
		if strings.Contains(h, needle) {
			return i
		}
	}
	return -1
}

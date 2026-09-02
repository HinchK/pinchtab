//go:build windows

package browserprobe

import (
	"os"
	"path/filepath"
)

// versionFromInstallLayout reads Chrome's version from its install layout instead
// of running the binary.
//
// On Windows `chrome.exe --version` does not print a version: it hands the
// arguments to the already-running browser and prints "Opening in existing browser
// session.", so the exec probe both fails to parse AND pops a browser window open
// during `pinchtab doctor`. Chrome's installer always places a versioned directory
// beside chrome.exe:
//
//	C:\Program Files\Google\Chrome\Application\chrome.exe
//	C:\Program Files\Google\Chrome\Application\151.0.7922.175\
//
// Read that name. Returns "" when the layout does not match, which leaves
// RunVersion's exec probe as the fallback.
func versionFromInstallLayout(binary string) string {
	entries, err := os.ReadDir(filepath.Dir(binary))
	if err != nil {
		return ""
	}
	best := ""
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !hasDottedDigits(name) {
			continue
		}
		if best == "" || CompareSemver(name, best) > 0 {
			best = name
		}
	}
	return best
}

//go:build windows

package browserprobe

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// defaultPATHEXT mirrors the Windows default when the environment does not set
// PATHEXT. Only the extensions that can name a browser launcher matter here.
const defaultPATHEXT = ".COM;.EXE;.BAT;.CMD"

// IsExecutable reports whether a probed path is runnable.
//
// Windows has no execute permission bit: os.Stat reports every readable file as
// -rw-rw-rw-, so a unix-style mode check rejects every candidate. Chrome's
// installer does not put chrome.exe on PATH, which left the entire per-OS
// fallback list in chrome.CommonPaths("windows") unreachable — PinchTab reported
// "no chrome browser found on this host" on machines with Chrome installed at the
// default location. Decide by extension instead, the same way exec.LookPath does.
func IsExecutable(_ fs.FileInfo, path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return false
	}
	pathExt := os.Getenv("PATHEXT")
	if strings.TrimSpace(pathExt) == "" {
		pathExt = defaultPATHEXT
	}
	for _, candidate := range strings.Split(pathExt, ";") {
		if strings.ToLower(strings.TrimSpace(candidate)) == ext {
			return true
		}
	}
	return false
}

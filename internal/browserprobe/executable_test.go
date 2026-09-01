package browserprobe

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A browser at a common install path must be discovered without being on PATH.
// On Windows this failed for every candidate: os.Stat reports no execute bits
// there, so the mode check rejected the whole per-OS fallback list and PinchTab
// reported "no chrome browser found on this host" on machines with Chrome
// installed at the default location.
func TestDiscoverBinaryAcceptsPlatformExecutableFallback(t *testing.T) {
	dir := t.TempDir()
	name := "fallback-browser"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	fallback := filepath.Join(dir, name)
	if err := os.WriteFile(fallback, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())

	got := DiscoverBinary([]string{"definitely-not-installed"}, []string{fallback})
	if got.Found != fallback {
		t.Fatalf("Found = %q, want %q (probed %v)", got.Found, fallback, got.Probed)
	}
}

func TestIsExecutableRejectsNonExecutableCandidates(t *testing.T) {
	dir := t.TempDir()

	// A name the platform would not run: no extension on Windows, no mode bits
	// on unix.
	name := "not-a-program"
	perm := os.FileMode(0o644)
	if runtime.GOOS == "windows" {
		name = "not-a-program.txt"
		perm = 0o666
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("data"), perm); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if IsExecutable(info, path) {
		t.Fatalf("IsExecutable(%q) = true, want false", path)
	}
}

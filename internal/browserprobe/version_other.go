//go:build !windows

package browserprobe

// versionFromInstallLayout has no unix implementation: `chrome --version` prints
// the version on stdout there, so RunVersion's exec probe is authoritative.
func versionFromInstallLayout(string) string { return "" }

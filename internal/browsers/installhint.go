package browsers

// InstallHint returns the platform-appropriate way to obtain a Chrome/Chromium
// build, for use in "no browser found" diagnostics.
//
// The advice used to be `apt-get install -y chromium` unconditionally, which is
// wrong on the two platforms where a user is most likely to hit it: PinchTab
// publishes windows and darwin binaries, and a blocked operator on either one was
// handed a Debian command.
func InstallHint(goos string) string {
	switch goos {
	case "linux":
		return "`apt-get install -y chromium` on Debian/Ubuntu, or install Google Chrome for Testing"
	case "darwin":
		return "`brew install --cask chromium`, or install Google Chrome for Testing " +
			"(preferred over your daily Chrome for automation)"
	case "windows":
		return "`winget install Google.Chrome`, or download Google Chrome for Testing"
	default:
		return "install Google Chrome, Chromium, or Google Chrome for Testing"
	}
}

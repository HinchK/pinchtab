package stealth

import (
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"
	"testing"
)

func hostVersionOracle(t *testing.T) (uaDataPlatform, version string) {
	t.Helper()
	switch goruntime.GOOS {
	case "darwin":
		out, err := exec.Command("sw_vers", "-productVersion").Output()
		if err != nil {
			t.Skipf("sw_vers unavailable: %v", err)
		}
		return "macOS", strings.TrimSpace(string(out))
	case "linux":
		out, err := exec.Command("uname", "-r").Output()
		if err != nil {
			t.Skipf("uname unavailable: %v", err)
		}
		return "Linux", strings.TrimSpace(string(out))
	default:
		t.Skipf("no host version oracle for %s", goruntime.GOOS)
		return "", ""
	}
}

func TestPlatformVersionForHostPlatformTracksHost(t *testing.T) {
	uaDataPlatform, raw := hostVersionOracle(t)
	want := normalizePlatformVersion(raw)
	if want == "" {
		t.Fatalf("oracle produced no version from %q", raw)
	}
	if got := PlatformVersionFor(uaDataPlatform); got != want {
		t.Fatalf("PlatformVersionFor(%q) = %q, want host version %q", uaDataPlatform, got, want)
	}
}

// Windows is the one platform whose platformVersion must NOT come from the host,
// and the reason lives here rather than in a comment because a comment cannot
// fail. Chrome's Sec-CH-UA-Platform-Version on Windows is derived from the
// UniversalApiContract version, not the OS version: Windows 11 22H2 reports
// 15.0.0 while its OS version is 10.0.22621. A Windows arm reading host data would
// emit something like 10.0.26100 — a value real Chrome never sends — making the
// fingerprint worse than the constant it replaced.
//
// hostPlatformVersion has no Windows branch, so this exercises the same code path
// on every GOOS: the guard is real on the macOS and Linux runners that exist, and
// its value does not wait for a Windows runner. No build tag, no skip, no oracle —
// a guard that skips is the failure mode this test exists to close.
func TestWindowsPlatformVersionIsNotDerivedFromTheHostBecauseChromeSendsAnAPIContractVersion(t *testing.T) {
	if got := hostPlatformVersion("Windows"); got != "" {
		t.Errorf("hostPlatformVersion(\"Windows\") = %q, want empty.\n"+
			"Chrome derives Sec-CH-UA-Platform-Version on Windows from the UniversalApiContract version, not the OS version "+
			"(Windows 11 22H2 sends 15.0.0 while its OS version is 10.0.22621), so a host-derived value is one real Chrome never sends. "+
			"Completing the switch with a Windows arm makes the persona easier to detect, not harder — the constant is the correct answer here.", got)
	}

	// The rule is about where the value comes from, not about emptiness: the
	// advertised Windows version stays the frozen default, so this test cannot be
	// satisfied by making PlatformVersionFor return nothing.
	if got, want := PlatformVersionFor("Windows"), "15.0.0"; got != want {
		t.Errorf("PlatformVersionFor(%q) = %q, want the frozen %q that matches what Chrome sends", "Windows", got, want)
	}

	// The assertion above cannot see the edit this test exists to stop. A Windows
	// arm gated on GOOS == "windows" — the shape a contributor "completing the
	// switch" would write — never executes on a macOS or Linux runner, so calling
	// hostPlatformVersion here returns empty either way and the guard would pass on
	// every machine this project actually tests on. The arms themselves are what
	// must be checked, and reading them is GOOS-independent.
	raw, err := os.ReadFile("ua.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)[strings.Index(string(raw), "func hostPlatformVersion("):]
	if end := strings.Index(body, "\nfunc "); end >= 0 {
		body = body[:end]
	}
	if strings.Contains(body, `"Windows"`) {
		t.Errorf("hostPlatformVersion gained a Windows arm:\n%s\n"+
			"Chrome derives Sec-CH-UA-Platform-Version on Windows from the UniversalApiContract version, not the OS version, "+
			"so any host-derived value there is one real Chrome never sends. Leave Windows on the frozen default.", body)
	}
	for _, hostArm := range []string{`"macOS" && goruntime.GOOS == "darwin"`, `"Linux" && goruntime.GOOS == "linux"`} {
		if !strings.Contains(body, hostArm) {
			t.Errorf("hostPlatformVersion no longer reads the host for %s; this test would then be pinning an empty switch rather than the Windows exception", hostArm)
		}
	}
}

func TestPlatformVersionForForeignPlatformUsesDefault(t *testing.T) {
	foreign := "Windows"
	if goruntime.GOOS == "windows" {
		foreign = "macOS"
	}
	if got := PlatformVersionFor(foreign); got != defaultPlatformVersions[foreign] {
		t.Fatalf("PlatformVersionFor(%q) = %q, want default %q", foreign, got, defaultPlatformVersions[foreign])
	}
	if got := PlatformVersionFor("Android"); got != "" {
		t.Fatalf("PlatformVersionFor unknown platform = %q, want empty", got)
	}
}

func TestNormalizePlatformVersion(t *testing.T) {
	cases := map[string]string{
		"26.5.1":                 "26.5.1",
		"26.5":                   "26.5.0",
		"15":                     "15.0.0",
		"6.5.0-27-generic":       "6.5.0",
		"6.11.0-19-lowlatency":   "6.11.0",
		"10.0.22631 Build 22631": "10.0.22631",
		"":                       "",
		"unknown":                "",
		"14.7.1.2":               "14.7.1",
	}
	for raw, want := range cases {
		if got := normalizePlatformVersion(raw); got != want {
			t.Errorf("normalizePlatformVersion(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestBuildPersonaPlatformVersionMatchesPlatform(t *testing.T) {
	windows := BuildPersona("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36", "144.0.7559.133")
	if windows.UserAgentData.Platform != "Windows" {
		t.Fatalf("platform = %q, want Windows", windows.UserAgentData.Platform)
	}
	if windows.UserAgentData.PlatformVersion != PlatformVersionFor("Windows") {
		t.Fatalf("platformVersion = %q, want %q", windows.UserAgentData.PlatformVersion, PlatformVersionFor("Windows"))
	}

	uaDataPlatform, _ := hostVersionOracle(t)
	hostUA := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36"
	if uaDataPlatform == "Linux" {
		hostUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36"
	}
	persona := BuildPersona(hostUA, "144.0.7559.133")
	if persona.UserAgentData.PlatformVersion != PlatformVersionFor(uaDataPlatform) {
		t.Fatalf("host persona platformVersion = %q, want %q", persona.UserAgentData.PlatformVersion, PlatformVersionFor(uaDataPlatform))
	}
}

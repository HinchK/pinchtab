package stealth

import (
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
	if got := PlatformVersionFor(uaDataPlatform); got == defaultPlatformVersions[uaDataPlatform] && want != defaultPlatformVersions[uaDataPlatform] {
		t.Fatalf("PlatformVersionFor(%q) returned the frozen default", uaDataPlatform)
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

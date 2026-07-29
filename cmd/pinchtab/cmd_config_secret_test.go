package main

import "testing"

// `pinchtab config get <path>` prints the value, and `config set` echoes it
// back, so every config path holding secret material must be masked. The
// schema's secret-bearing fields are capsolverKey, twoCaptchaKey,
// stateEncryptionKey, password, token and the credentials subtree.
func TestIsSensitiveConfigPathCoversEverySecretField(t *testing.T) {
	sensitive := []string{
		"server.token",
		"browser.proxy.password",
		"security.stateEncryptionKey",
		"autoSolver.external.capsolverKey",
		"autoSolver.external.twoCaptchaKey",
		"autoSolver.credentials.login.password",
		"autoSolver.credentials.signup.email",
		"browser.targets.default.proxy.password",
	}
	for _, path := range sensitive {
		if !isSensitiveConfigPath(path) {
			t.Errorf("isSensitiveConfigPath(%q) = false, want true — value would print in clear", path)
		}
	}
}

// Masking must stay narrow enough that ordinary settings remain readable.
func TestIsSensitiveConfigPathLeavesOrdinarySettingsVisible(t *testing.T) {
	ordinary := []string{
		"server.port",
		"server.bind",
		"browser.targets.default.provider",
		"instanceDefaults.mode",
		"scheduler.maxInflight",
		"security.allowEvaluate",
		"observability.activity.retentionDays",
	}
	for _, path := range ordinary {
		if isSensitiveConfigPath(path) {
			t.Errorf("isSensitiveConfigPath(%q) = true, want false — ordinary setting would be masked", path)
		}
	}
}

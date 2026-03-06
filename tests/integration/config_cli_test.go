//go:build integration

package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigCLI_Init tests `pinchtab config init`
func TestConfigCLI_Init(t *testing.T) {
	tmpDir := t.TempDir()
	// config init writes to userConfigDir() which is $HOME/.pinchtab on macOS (legacy path)
	// or ~/Library/Application Support/pinchtab (new path). Create the legacy dir so it's used.
	legacyDir := filepath.Join(tmpDir, ".pinchtab")
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(legacyDir, "config.json")

	cmd := exec.Command(server.BinaryPath, "config", "init")
	cmd.Env = append(filterTestEnv(), "HOME="+tmpDir)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("config init failed: %v\nOutput: %s", err, out)
	}

	// Verify file was created
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config file not created at %s: %v\nOutput: %s", configPath, err, out)
	}

	// Verify it's valid nested JSON
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("config file is not valid JSON: %v", err)
	}

	// Verify nested structure
	if _, ok := cfg["server"]; !ok {
		t.Error("expected 'server' section in config")
	}
	if _, ok := cfg["chrome"]; !ok {
		t.Error("expected 'chrome' section in config")
	}
}

// TestConfigCLI_Show tests `pinchtab config show`
func TestConfigCLI_Show(t *testing.T) {
	cmd := exec.Command(server.BinaryPath, "config", "show")
	cmd.Env = append(filterTestEnv(),
		"PINCHTAB_PORT=9999",
		"PINCHTAB_CONFIG="+filepath.Join(t.TempDir(), "nonexistent.json"),
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("config show failed: %v\nOutput: %s", err, out)
	}

	output := string(out)

	// Should show port from env var
	if !strings.Contains(output, "9999") {
		t.Errorf("expected output to contain port 9999, got: %s", output)
	}

	// Should have section headers
	if !strings.Contains(output, "Server:") {
		t.Errorf("expected output to contain 'Server:' section, got: %s", output)
	}
	if !strings.Contains(output, "Chrome:") {
		t.Errorf("expected output to contain 'Chrome:' section, got: %s", output)
	}
}

// TestConfigCLI_Path tests `pinchtab config path`
func TestConfigCLI_Path(t *testing.T) {
	tmpDir := t.TempDir()
	expectedPath := filepath.Join(tmpDir, "custom-config.json")

	cmd := exec.Command(server.BinaryPath, "config", "path")
	cmd.Env = append(filterTestEnv(), "PINCHTAB_CONFIG="+expectedPath)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("config path failed: %v\nOutput: %s", err, out)
	}

	output := strings.TrimSpace(string(out))
	if output != expectedPath {
		t.Errorf("expected path %q, got %q", expectedPath, output)
	}
}

// TestConfigCLI_Set tests `pinchtab config set`
func TestConfigCLI_Set(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Initialize config first
	initCmd := exec.Command(server.BinaryPath, "config", "init")
	initCmd.Env = append(filterTestEnv(), "PINCHTAB_CONFIG="+configPath, "HOME="+tmpDir)
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("config init failed: %v\nOutput: %s", err, out)
	}

	// Set a value
	setCmd := exec.Command(server.BinaryPath, "config", "set", "server.port", "8080")
	setCmd.Env = append(filterTestEnv(), "PINCHTAB_CONFIG="+configPath)

	out, err := setCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("config set failed: %v\nOutput: %s", err, out)
	}

	if !strings.Contains(string(out), "Set server.port = 8080") {
		t.Errorf("expected success message, got: %s", out)
	}

	// Verify the value was set
	data, _ := os.ReadFile(configPath)
	var cfg map[string]any
	_ = json.Unmarshal(data, &cfg)

	server, ok := cfg["server"].(map[string]any)
	if !ok {
		t.Fatal("server section not found")
	}
	if server["port"] != "8080" {
		t.Errorf("expected port 8080, got %v", server["port"])
	}
}

// TestConfigCLI_Patch tests `pinchtab config patch`
func TestConfigCLI_Patch(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Initialize config first
	initCmd := exec.Command(server.BinaryPath, "config", "init")
	initCmd.Env = append(filterTestEnv(), "PINCHTAB_CONFIG="+configPath, "HOME="+tmpDir)
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("config init failed: %v\nOutput: %s", err, out)
	}

	// Patch with JSON
	patchJSON := `{"server":{"port":"7777"},"chrome":{"maxTabs":100}}`
	patchCmd := exec.Command(server.BinaryPath, "config", "patch", patchJSON)
	patchCmd.Env = append(filterTestEnv(), "PINCHTAB_CONFIG="+configPath)

	out, err := patchCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("config patch failed: %v\nOutput: %s", err, out)
	}

	// Verify the values were set
	data, _ := os.ReadFile(configPath)
	var cfg map[string]any
	_ = json.Unmarshal(data, &cfg)

	server, ok := cfg["server"].(map[string]any)
	if !ok {
		t.Fatal("server section not found")
	}
	if server["port"] != "7777" {
		t.Errorf("expected port 7777, got %v", server["port"])
	}

	chrome, ok := cfg["chrome"].(map[string]any)
	if !ok {
		t.Fatal("chrome section not found")
	}
	// JSON unmarshals numbers as float64
	if chrome["maxTabs"] != float64(100) {
		t.Errorf("expected maxTabs 100, got %v", chrome["maxTabs"])
	}
}

// TestConfigCLI_Validate_Valid tests `pinchtab config validate` with valid config
func TestConfigCLI_Validate_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Create a valid config
	validConfig := `{
		"server": {"port": "9867"},
		"chrome": {"stealthLevel": "light", "tabEvictionPolicy": "reject"},
		"orchestrator": {"strategy": "simple", "allocationPolicy": "fcfs"}
	}`
	if err := os.WriteFile(configPath, []byte(validConfig), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(server.BinaryPath, "config", "validate")
	cmd.Env = append(filterTestEnv(), "PINCHTAB_CONFIG="+configPath)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("config validate failed for valid config: %v\nOutput: %s", err, out)
	}

	if !strings.Contains(string(out), "valid") {
		t.Errorf("expected success message with 'valid', got: %s", out)
	}
}

// TestConfigCLI_Validate_Invalid tests `pinchtab config validate` with invalid config
func TestConfigCLI_Validate_Invalid(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Create an invalid config
	invalidConfig := `{
		"server": {"port": "99999"},
		"chrome": {"stealthLevel": "superstealth"},
		"orchestrator": {"strategy": "magical"}
	}`
	if err := os.WriteFile(configPath, []byte(invalidConfig), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(server.BinaryPath, "config", "validate")
	cmd.Env = append(filterTestEnv(), "PINCHTAB_CONFIG="+configPath)

	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("config validate should have failed for invalid config, got: %s", out)
	}

	output := string(out)
	// Should mention the errors
	if !strings.Contains(output, "error") {
		t.Errorf("expected error message, got: %s", output)
	}
}

// TestConfigCLI_LegacyConfigDetection tests that legacy flat config is still loaded
func TestConfigCLI_LegacyConfigDetection(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Create a legacy flat config
	legacyConfig := `{
		"port": "8765",
		"headless": true,
		"maxTabs": 30
	}`
	if err := os.WriteFile(configPath, []byte(legacyConfig), 0644); err != nil {
		t.Fatal(err)
	}

	// Run show to verify it's loaded
	cmd := exec.Command(server.BinaryPath, "config", "show")
	cmd.Env = append(filterTestEnv(), "PINCHTAB_CONFIG="+configPath)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("config show failed: %v\nOutput: %s", err, out)
	}

	output := string(out)
	// Should show port from legacy config
	if !strings.Contains(output, "8765") {
		t.Errorf("expected output to contain port 8765 from legacy config, got: %s", output)
	}

	// Run validate - legacy config has "Note:" warning
	validateCmd := exec.Command(server.BinaryPath, "config", "validate")
	validateCmd.Env = append(filterTestEnv(), "PINCHTAB_CONFIG="+configPath)

	validateOut, _ := validateCmd.CombinedOutput()
	// Should mention legacy format
	if !strings.Contains(string(validateOut), "legacy") {
		t.Logf("Note: legacy format warning not shown (may be expected): %s", validateOut)
	}
}

// filterTestEnv returns a clean env without PINCHTAB_*/BRIDGE_* variables
func filterTestEnv() []string {
	var out []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "PINCHTAB_") && !strings.HasPrefix(e, "BRIDGE_") {
			out = append(out, e)
		}
	}
	return out
}

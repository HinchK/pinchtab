package main

import (
	"reflect"
	"testing"

	"github.com/pinchtab/pinchtab/internal/config"
)

func TestApplyServerAddressFlagsPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		bind     string
		port     string
		wantBind string
		wantPort string
	}{
		{name: "no flags leaves config", wantBind: "127.0.0.1", wantPort: "9867"},
		{name: "port flag wins", port: "9880", wantBind: "127.0.0.1", wantPort: "9880"},
		{name: "bind flag wins", bind: "0.0.0.0", wantBind: "0.0.0.0", wantPort: "9867"},
		{name: "both flags win", bind: "0.0.0.0", port: "9880", wantBind: "0.0.0.0", wantPort: "9880"},
		{name: "blank flags leave config", bind: "   ", port: "  ", wantBind: "127.0.0.1", wantPort: "9867"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.RuntimeConfig{Bind: "127.0.0.1", Port: "9867"}
			applyServerAddressFlags(cfg, tt.bind, tt.port)
			if cfg.Bind != tt.wantBind {
				t.Errorf("Bind = %q, want %q", cfg.Bind, tt.wantBind)
			}
			if cfg.Port != tt.wantPort {
				t.Errorf("Port = %q, want %q", cfg.Port, tt.wantPort)
			}
		})
	}
}

// The server command must accept the same address overrides as `pinchtab
// bridge`, or the two server modes disagree about how to move off the config
// port — which is what the not-running guidance now tells users to do.
func TestServerCmdAddressFlagsMatchBridge(t *testing.T) {
	for _, name := range []string{"bind", "port"} {
		serverFlag := serverCmd.Flags().Lookup(name)
		if serverFlag == nil {
			t.Fatalf("server has no --%s flag", name)
		}
		bridgeFlag := bridgeCmd.Flags().Lookup(name)
		if bridgeFlag == nil {
			t.Fatalf("bridge has no --%s flag", name)
		}
		if serverFlag.Value.Type() != bridgeFlag.Value.Type() {
			t.Errorf("--%s type = %q, want bridge's %q", name, serverFlag.Value.Type(), bridgeFlag.Value.Type())
		}
		if serverFlag.DefValue != "" {
			t.Errorf("--%s default = %q, want empty so config keeps winning", name, serverFlag.DefValue)
		}
	}
}

// The detached child re-parses its own flags, so an address override applied in
// the parent has to travel with it; otherwise the parent waits on a URL the
// child never binds.
func TestBackgroundServerArgsForwardsAddressFlags(t *testing.T) {
	got := backgroundServerArgs("marker-123", serverBackgroundOptions{
		Bind: "0.0.0.0",
		Port: "9880",
	})
	want := []string{
		"server", "--background-child", "marker-123",
		"--bind", "0.0.0.0",
		"--port", "9880",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("backgroundServerArgs() = %#v, want %#v", got, want)
	}
}

package handlers

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	coreautosolver "github.com/pinchtab/pinchtab/internal/autosolver"
	"github.com/pinchtab/pinchtab/internal/autosolver/catalog"
	"github.com/pinchtab/pinchtab/internal/config"
)

func TestLLMProviderForAutoSolver(t *testing.T) {
	// LLM provider configured → instantiated (wires the llmFallback switch).
	h := &Handlers{Config: &config.RuntimeConfig{
		AutoSolver: config.AutoSolverConfig{LLMProvider: "openai"},
	}}
	if h.llmProviderForAutoSolver() == nil {
		t.Error("expected non-nil LLM provider when LLMProvider is set")
	}

	// No provider configured → nil (LLM branch stays inert, as before).
	h = &Handlers{Config: &config.RuntimeConfig{}}
	if h.llmProviderForAutoSolver() != nil {
		t.Error("expected nil LLM provider when LLMProvider is empty")
	}

	// nil Config → nil.
	if (&Handlers{}).llmProviderForAutoSolver() != nil {
		t.Error("expected nil LLM provider with nil Config")
	}
}

func TestShouldAutoSolve(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.RuntimeConfig
		trigger string
		want    bool
	}{
		{
			name:    "nil config",
			cfg:     nil,
			trigger: autoSolverTriggerNavigate,
			want:    false,
		},
		{
			name: "disabled autosolver",
			cfg: &config.RuntimeConfig{AutoSolver: config.AutoSolverConfig{
				Enabled:           false,
				AutoTrigger:       true,
				TriggerOnNavigate: true,
				TriggerOnAction:   true,
			}},
			trigger: autoSolverTriggerNavigate,
			want:    false,
		},
		{
			name: "auto trigger disabled",
			cfg: &config.RuntimeConfig{AutoSolver: config.AutoSolverConfig{
				Enabled:           true,
				AutoTrigger:       false,
				TriggerOnNavigate: true,
				TriggerOnAction:   true,
			}},
			trigger: autoSolverTriggerNavigate,
			want:    false,
		},
		{
			name: "navigate trigger enabled",
			cfg: &config.RuntimeConfig{AutoSolver: config.AutoSolverConfig{
				Enabled:           true,
				AutoTrigger:       true,
				TriggerOnNavigate: true,
				TriggerOnAction:   false,
			}},
			trigger: autoSolverTriggerNavigate,
			want:    true,
		},
		{
			name: "action trigger disabled",
			cfg: &config.RuntimeConfig{AutoSolver: config.AutoSolverConfig{
				Enabled:           true,
				AutoTrigger:       true,
				TriggerOnNavigate: true,
				TriggerOnAction:   false,
			}},
			trigger: autoSolverTriggerAction,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handlers{Config: tt.cfg}
			if got := h.shouldAutoSolve(tt.trigger); got != tt.want {
				t.Fatalf("shouldAutoSolve(%q) = %v, want %v", tt.trigger, got, tt.want)
			}
		})
	}
}

func TestMaybeAutoSolve_InvokesRunnerWhenEnabled(t *testing.T) {
	h := &Handlers{
		Config: &config.RuntimeConfig{AutoSolver: config.AutoSolverConfig{
			Enabled:           true,
			AutoTrigger:       true,
			TriggerOnNavigate: true,
			TriggerOnAction:   true,
		}},
	}

	var calls atomic.Int64
	done := make(chan struct{}, 8)
	h.autoSolverRunner = func(_ context.Context, tabID string) error {
		calls.Add(1)
		if tabID != "tab1" {
			t.Errorf("runner tabID = %q, want tab1", tabID)
		}
		done <- struct{}{}
		return nil
	}

	waitFor := func(expected int64) bool {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if calls.Load() == expected {
				return true
			}
			time.Sleep(5 * time.Millisecond)
		}
		return false
	}

	h.maybeAutoSolve(context.Background(), "tab1", autoSolverTriggerNavigate)
	if !waitFor(1) {
		t.Fatalf("autoSolverRunner calls = %d, want 1", calls.Load())
	}
	<-done

	h.maybeAutoSolve(context.Background(), "", autoSolverTriggerNavigate)
	time.Sleep(20 * time.Millisecond) // ensure no goroutine was spawned
	if got := calls.Load(); got != 1 {
		t.Fatalf("autoSolverRunner calls with empty tab id = %d, want unchanged", got)
	}

	h.Config.AutoSolver.TriggerOnNavigate = false
	h.maybeAutoSolve(context.Background(), "tab1", autoSolverTriggerNavigate)
	time.Sleep(20 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("autoSolverRunner calls with navigate trigger disabled = %d, want unchanged", got)
	}
}

// The catalog is only a single owner if what actually registers stays inside it.
// A new solver wired into buildAutoSolver but not added to the catalog would
// leave config validation rejecting a name the product really accepts, and this
// is the link that fails when that happens.
func TestRegisteredSolversAreAllKnownToTheCatalog(t *testing.T) {
	h := &Handlers{Config: &config.RuntimeConfig{AutoSolver: config.AutoSolverConfig{
		CapsolverKey:  "test-capsolver-key",
		TwoCaptchaKey: "test-twocaptcha-key",
	}}}

	as := h.buildAutoSolver(coreautosolver.DefaultConfig(), true)
	registered := as.Registry().Names()
	if len(registered) == 0 {
		t.Fatal("no solvers registered — this guard is checking nothing")
	}

	for _, name := range registered {
		if !catalog.IsKnown(name) {
			t.Errorf("solver %q registers but config validation rejects it (known: %v)", name, catalog.Names())
		}
	}

	// Both key-gated solvers registered above, so the catalog's key-gated list is
	// the real one rather than a guess.
	for _, gated := range catalog.KeyGated() {
		if !slices.Contains(registered, gated) {
			t.Errorf("catalog lists %q as key-gated but it did not register with a key set (registered: %v)", gated, registered)
		}
	}
}

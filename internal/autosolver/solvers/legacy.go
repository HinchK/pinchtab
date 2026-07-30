package solvers

import (
	"context"

	"github.com/pinchtab/pinchtab/internal/autosolver"
	legacySolver "github.com/pinchtab/pinchtab/internal/solver"
)

// LegacyAdapter wraps an existing solver.Solver (PR #395 interface) to
// work with the new autosolver.Solver interface. This allows the existing
// CloudflareSolver in bridge/cloudflare.go to be used alongside new
// autosolver implementations during the migration period.
//
// Because the legacy Solver interface requires a chromedp context directly
// (via ctx), this adapter only works when the ActionExecutor is backed by
// a chromedp context (i.e., the PinchtabExecutor from the adapters package).
type LegacyAdapter struct {
	solver   legacySolver.Solver
	priority int
}

// NewLegacyAdapter wraps a legacy solver.Solver.
func NewLegacyAdapter(s legacySolver.Solver, priority int) *LegacyAdapter {
	return &LegacyAdapter{solver: s, priority: priority}
}

func (a *LegacyAdapter) Name() string  { return a.solver.Name() }
func (a *LegacyAdapter) Priority() int { return a.priority }

// CanHandle delegates to the legacy solver's CanHandle.
// Note: The legacy interface passes ctx directly (which must be a chromedp context).
// This adapter uses the same ctx, which works when the Page is backed by chromedp.
func (a *LegacyAdapter) CanHandle(ctx context.Context, _ autosolver.Page) (bool, error) {
	return a.solver.CanHandle(ctx)
}

// Solve delegates to the legacy solver's Solve method. Both paths report
// a.Name() as the solver identity: legacyResult.Solver is json:"omitempty" and a
// legacy solver may leave it unset, which reported an empty name on success.
func (a *LegacyAdapter) Solve(ctx context.Context, page autosolver.Page, _ autosolver.ActionExecutor) (*autosolver.Result, error) {
	legacyResult, err := a.solver.Solve(ctx, legacySolver.Options{MaxAttempts: 3})
	if err != nil {
		return &autosolver.Result{
			SolverUsed: a.Name(),
			Error:      err.Error(),
		}, err
	}

	return &autosolver.Result{
		Solved:     legacyResult.Solved,
		SolverUsed: a.Name(),
		Attempts:   legacyResult.Attempts,
		FinalTitle: legacyResult.Title,
	}, nil
}

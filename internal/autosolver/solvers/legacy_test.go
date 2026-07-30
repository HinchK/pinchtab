package solvers

import (
	"context"
	"errors"
	"testing"

	legacySolver "github.com/pinchtab/pinchtab/internal/solver"
)

// stubLegacySolver reports a Result whose Solver field is unset, which
// json:"solver,omitempty" makes a legitimate shape for a legacy solver.
type stubLegacySolver struct {
	name     string
	result   *legacySolver.Result
	solveErr error
}

func (s *stubLegacySolver) Name() string                            { return s.name }
func (s *stubLegacySolver) CanHandle(context.Context) (bool, error) { return true, nil }
func (s *stubLegacySolver) Solve(context.Context, legacySolver.Options) (*legacySolver.Result, error) {
	return s.result, s.solveErr
}

func TestLegacyAdapterReportsItsOwnNameOnBothPaths(t *testing.T) {
	tests := []struct {
		name    string
		stub    *stubLegacySolver
		wantErr bool
	}{
		{
			name: "success with an unset Solver field",
			stub: &stubLegacySolver{name: "legacy-cf", result: &legacySolver.Result{Solved: true, Title: "done"}},
		},
		{
			name: "success with a Solver field the legacy solver did fill in",
			stub: &stubLegacySolver{name: "legacy-cf", result: &legacySolver.Result{Solved: true, Solver: "legacy-cf"}},
		},
		{
			name:    "failure",
			stub:    &stubLegacySolver{name: "legacy-cf", solveErr: errors.New("boom")},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			adapter := NewLegacyAdapter(tc.stub, 5)
			result, err := adapter.Solve(context.Background(), nil, nil)
			if tc.wantErr != (err != nil) {
				t.Fatalf("Solve() error = %v, wantErr %v", err, tc.wantErr)
			}
			if result == nil {
				t.Fatal("Solve returned no Result to check")
			}
			// The oracle is a literal, not adapter.Name(): production reads that same
			// accessor for this field, so comparing against it pins only WHERE the
			// value comes from. A Name() returning "" satisfied the accessor form on
			// all three cases while every legacy result reported an empty identity.
			if result.SolverUsed != "legacy-cf" {
				t.Errorf("Result.SolverUsed = %q, want %q — the adapter must report its own identity on every path", result.SolverUsed, "legacy-cf")
			}
		})
	}
}

package external

import (
	"context"
	"testing"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

// The reported solver identity has to be the registered one. Both were spelled as
// literals beside a Name() that already read the constant, so renaming the constant
// changed what registers while leaving what the Result reports — and nothing in the
// tree noticed. These solvers are skeletons today, which is why it stayed latent
// rather than shipping a wrong name; it is also why it would have survived until
// the first real implementation.
func TestSolverUsedMatchesTheRegisteredName(t *testing.T) {
	for _, solver := range []autosolver.Solver{
		NewCapsolver(CapsolverConfig{}),
		NewTwoCaptcha(TwoCaptchaConfig{}),
	} {
		t.Run(solver.Name(), func(t *testing.T) {
			// An unset API key is the earliest return, and it is enough: the
			// Result is built before the check.
			result, err := solver.Solve(context.Background(), nil, nil)
			if err == nil {
				t.Fatal("expected the unset-API-key error, so this test exercises the real Solve path")
			}
			if result == nil {
				t.Fatal("Solve returned no Result to check")
			}
			if result.SolverUsed != solver.Name() {
				t.Errorf("Result.SolverUsed = %q but Name() = %q — the reported solver identity has drifted from the registered one", result.SolverUsed, solver.Name())
			}
		})
	}
}

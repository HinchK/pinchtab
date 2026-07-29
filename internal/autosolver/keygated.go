package autosolver

const (
	CapsolverSolverName  = "capsolver"
	TwoCaptchaSolverName = "twocaptcha"
)

// KeyGatedSolver is a solver that only registers once its API key is set, paired
// with the config key that sets it. Naming one without its key is legal config,
// so the runtime says which key is missing rather than the config refusing to
// load. This is the single owner of both facts: catalog.KeyGated() and the
// registration sites read it instead of restating names.
type KeyGatedSolver struct {
	Name      string
	ConfigKey string
}

func KeyGatedSolvers() []KeyGatedSolver {
	return []KeyGatedSolver{
		{Name: CapsolverSolverName, ConfigKey: "autoSolver.external.capsolverKey"},
		{Name: TwoCaptchaSolverName, ConfigKey: "autoSolver.external.twoCaptchaKey"},
	}
}

func keyGatedSolverNamed(name string) (KeyGatedSolver, bool) {
	for _, gated := range KeyGatedSolvers() {
		if gated.Name == name {
			return gated, true
		}
	}
	return KeyGatedSolver{}, false
}

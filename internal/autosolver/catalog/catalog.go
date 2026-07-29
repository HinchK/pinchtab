// Package catalog is the single owner of the names autoSolver.solvers accepts.
// Every name comes from the thing that answers to it — the solver types' own
// Name() methods, and the built-in stage's exported name — so adding a solver
// cannot leave config validation stale behind a second hand-written list.
package catalog

import (
	"sort"

	"github.com/pinchtab/pinchtab/internal/autosolver"
	"github.com/pinchtab/pinchtab/internal/autosolver/external"
	"github.com/pinchtab/pinchtab/internal/autosolver/solvers"
)

// registrable is one instance of each solver type that can enter the registry,
// built with zero config because only Name() is read. KeyGated members are here
// too: their name is always valid, and whether they register is a separate
// question answered by the API key.
func registrable() []autosolver.Solver {
	return []autosolver.Solver{
		&solvers.Cloudflare{},
		&solvers.JSChallenge{},
		external.NewCapsolver(external.CapsolverConfig{}),
		external.NewTwoCaptcha(external.TwoCaptchaConfig{}),
	}
}

// KeyGated reports the solvers that only register when their API key is set.
// Naming one without its key is a missing-key mistake, not an unknown solver.
// The set itself is owned by autosolver.KeyGatedSolvers, which also carries the
// config key each one needs, so the runtime warning and this validation face of
// the same fact cannot drift.
func KeyGated() []string {
	gated := autosolver.KeyGatedSolvers()
	names := make([]string, 0, len(gated))
	for _, solver := range gated {
		names = append(names, solver.Name)
	}
	return names
}

// AlwaysRegistered reports the solvers that need no configuration to run.
func AlwaysRegistered() []string {
	gated := map[string]struct{}{}
	for _, name := range KeyGated() {
		gated[name] = struct{}{}
	}
	names := []string{autosolver.SemanticSolverName}
	for _, s := range registrable() {
		if _, ok := gated[s.Name()]; ok {
			continue
		}
		names = append(names, s.Name())
	}
	sort.Strings(names)
	return names
}

// Names is every value autoSolver.solvers may contain, sorted so error messages
// and tests read the same way every run.
func Names() []string {
	names := make([]string, 0, len(registrable())+1)
	for _, s := range registrable() {
		names = append(names, s.Name())
	}
	names = append(names, autosolver.SemanticSolverName)
	sort.Strings(names)
	return names
}

func IsKnown(name string) bool {
	for _, known := range Names() {
		if known == name {
			return true
		}
	}
	return false
}

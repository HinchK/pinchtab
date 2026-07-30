package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/autosolver"
	"github.com/pinchtab/pinchtab/internal/autosolver/catalog"
)

// documentMetadataSections are the two FileConfig members that are NOT configuration:
// they describe the document rather than the server. They are excluded here deliberately
// so nobody adds them to configSections to satisfy the walk below, and so nobody deletes
// the exclusion as an oversight.
var documentMetadataSections = map[string]bool{
	"$schema":       true,
	"configVersion": true,
}

// The criterion that makes this class of drift impossible. autoSolver and scheduler were
// both declared on FileConfig, honoured by the config file, documented, and exposed by
// the dashboard — and rejected by config set/get, because the section vocabulary was
// typed out by hand in four places and tied to nothing. This walks the struct instead,
// so a section added to FileConfig and nowhere else fails here rather than in an
// operator's terminal.
func TestEverySettableFileConfigSectionIsReachableFromBothResolvers(t *testing.T) {
	declared := declaredFileConfigSections(t)
	if len(declared) < 10 {
		t.Fatalf("found %d sections on FileConfig; this walk would be vacuous", len(declared))
	}

	table := map[string]bool{}
	for _, section := range configSections {
		table[section.name] = true
	}

	for _, name := range declared {
		if documentMetadataSections[name] {
			if table[name] {
				t.Errorf("%q is document metadata, not configuration, but configSections claims it", name)
			}
			continue
		}
		if !table[name] {
			t.Errorf("FileConfig declares section %q but configSections does not, so config set/get reject it — add it to the table rather than to a switch", name)
			continue
		}

		// Reachability is about the SECTION, not the field: drive a field name that
		// cannot exist and require the refusal to be about the field. An unknown-section
		// error here is the defect this test exists to catch.
		var fc FileConfig
		const bogusField = "definitelyNotAField"
		if err := SetConfigValue(&fc, name+"."+bogusField, "x"); err == nil {
			t.Errorf("set %s.%s was accepted; the section resolver validates nothing", name, bogusField)
		} else if strings.Contains(err.Error(), "unknown section") {
			t.Errorf("set %s.%s reports %v — the section is unreachable", name, bogusField, err)
		}
		if _, err := GetConfigValue(&fc, name+"."+bogusField); err == nil {
			t.Errorf("get %s.%s was accepted; the section resolver validates nothing", name, bogusField)
		} else if strings.Contains(err.Error(), "unknown section") {
			t.Errorf("get %s.%s reports %v — the section is unreachable", name, bogusField, err)
		}
	}

	// The other direction: a table entry naming a section FileConfig no longer declares
	// would leave the error message advertising a vocabulary that resolves to nothing.
	declaredSet := map[string]bool{}
	for _, name := range declared {
		declaredSet[name] = true
	}
	for _, section := range configSections {
		if !declaredSet[section.name] {
			t.Errorf("configSections lists %q, which FileConfig no longer declares", section.name)
		}
	}
}

// The schema walk must not be satisfiable by accepting everything, and the refusal has to
// keep naming the valid sections now that the list is generated rather than typed.
func TestABogusSectionIsStillRefusedAndTheRefusalNamesTheValidOnes(t *testing.T) {
	var fc FileConfig

	for _, path := range []string{"bogusSection.field", "autosolver.enabled", "Scheduler.enabled"} {
		setErr := SetConfigValue(&fc, path, "x")
		if setErr == nil {
			t.Errorf("set %s was accepted", path)
			continue
		}
		if !strings.Contains(setErr.Error(), "unknown section") {
			t.Errorf("set %s = %v, want an unknown-section refusal", path, setErr)
		}
		if _, getErr := GetConfigValue(&fc, path); getErr == nil {
			t.Errorf("get %s was accepted", path)
		}
	}

	err := SetConfigValue(&fc, "bogusSection.field", "x")
	// Generated from the table, so it can never advertise a vocabulary the resolvers
	// do not have — which is what the four hand-typed copies eventually did.
	for _, want := range []string{"server", "autoSolver", "scheduler"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not name the valid section %q", err, want)
		}
	}
}

// The key-gated solver warning hands an operator a config path to set. Nothing verified
// those hand-written strings named anything real: renaming the JSON tag they point at
// left autosolver, catalog, config and handlers all green, and the warning would then
// advise a key the config no longer reads. This is the cross-reference, and it only
// became possible once the editor knew the autoSolver section.
func TestEveryKeyGatedSolverConfigKeyResolves(t *testing.T) {
	names := catalog.KeyGated()
	if len(names) == 0 {
		t.Fatal("catalog reports no key-gated solvers; this cross-reference would be vacuous")
	}

	var fc FileConfig
	for _, name := range names {
		gated, ok := autosolver.KeyGatedSolverNamed(name)
		if !ok {
			t.Errorf("catalog.KeyGated lists %q but autosolver.KeyGatedSolverNamed does not know it", name)
			continue
		}
		if gated.ConfigKey == "" {
			t.Errorf("key-gated solver %q names no config key, so its warning cannot tell an operator what to set", name)
			continue
		}
		if _, err := GetConfigValue(&fc, gated.ConfigKey); err != nil {
			t.Errorf("solver %q points operators at %q, which config get rejects: %v", name, gated.ConfigKey, err)
		}
		if err := SetConfigValue(&fc, gated.ConfigKey, "probe"); err != nil {
			t.Errorf("solver %q points operators at %q, which config set rejects: %v", name, gated.ConfigKey, err)
		}
	}
}

func declaredFileConfigSections(t *testing.T) []string {
	t.Helper()
	typ := reflect.TypeOf(FileConfig{})
	names := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			t.Errorf("FileConfig field %s has no json tag, so it cannot be addressed by path", typ.Field(i).Name)
			continue
		}
		names = append(names, strings.Split(tag, ",")[0])
	}
	return names
}

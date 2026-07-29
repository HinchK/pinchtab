package catalog

import (
	"reflect"
	"sort"
	"testing"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

func TestKeyGatedMirrorsTheOwningSet(t *testing.T) {
	want := make([]string, 0)
	for _, gated := range autosolver.KeyGatedSolvers() {
		want = append(want, gated.Name)
		if gated.ConfigKey == "" {
			t.Errorf("key-gated solver %q has no config key, so the runtime warning cannot say what to set", gated.Name)
		}
	}
	if len(want) == 0 {
		t.Fatal("autosolver.KeyGatedSolvers is empty, so nothing pins this behaviour")
	}
	if got := KeyGated(); !reflect.DeepEqual(got, want) {
		t.Fatalf("KeyGated() = %v, want the owning set %v", got, want)
	}
}

func TestKeyGatedNamesAreValidConfigValues(t *testing.T) {
	for _, name := range KeyGated() {
		if !IsKnown(name) {
			t.Errorf("key-gated solver %q is rejected by config validation (known: %v)", name, Names())
		}
	}
}

func TestKeyGatedSolversAreExcludedFromAlwaysRegistered(t *testing.T) {
	gated := map[string]struct{}{}
	for _, name := range KeyGated() {
		gated[name] = struct{}{}
	}
	for _, name := range AlwaysRegistered() {
		if _, ok := gated[name]; ok {
			t.Errorf("%q is key-gated yet reported as always registered", name)
		}
	}
}

func TestRegistrableSolversAreAllKnownNames(t *testing.T) {
	names := Names()
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	if !reflect.DeepEqual(names, sorted) {
		t.Fatalf("Names() = %v, want sorted output so messages read the same every run", names)
	}
	for _, s := range registrable() {
		if !IsKnown(s.Name()) {
			t.Errorf("registrable solver %q is not a known config value (known: %v)", s.Name(), names)
		}
	}
}

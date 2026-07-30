package srccensus

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePackage(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// recorder stands in for *testing.T so a guard's own failures can be asserted. A census
// helper whose Fatal cannot be observed is a promise nobody checked.
type recorder struct {
	testing.TB
	fatals []string
}

func (r *recorder) Helper() {}
func (r *recorder) Fatalf(format string, args ...any) {
	r.fatals = append(r.fatals, sprintf(format, args...))
	panic(errStop{})
}

type errStop struct{}

func sprintf(format string, args ...any) string {
	return strings.TrimSpace(fmt.Sprintf(format, args...))
}

func mustFatal(t *testing.T, run func(tb testing.TB)) string {
	t.Helper()

	rec := &recorder{TB: t}
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				if _, ok := recovered.(errStop); !ok {
					panic(recovered)
				}
			}
		}()
		run(rec)
	}()
	if len(rec.fatals) == 0 {
		t.Fatal("expected the census helper to fail, but it returned quietly — the non-vacuity promise is not kept")
	}
	return rec.fatals[0]
}

// Non-vacuity is the property whose absence silently voids a guard, so it is asserted on
// the shared helper rather than trusted to each author.
func TestLoadFailsWhenThePackageYieldsFewerFilesThanTheGuardKnowsAbout(t *testing.T) {
	dir := writePackage(t, map[string]string{
		"only.go":      "package p\n",
		"only_test.go": "package p\n",
	})

	message := mustFatal(t, func(tb testing.TB) { Load(tb, dir, 2) })
	for _, needle := range []string{"parsed 1 non-test files", "want at least 2", "pass vacuously"} {
		if !strings.Contains(message, needle) {
			t.Errorf("failure %q does not carry %q", message, needle)
		}
	}
}

func TestLoadFailsOnSourceItCannotParse(t *testing.T) {
	dir := writePackage(t, map[string]string{"broken.go": "package p\nfunc f( {\n"})

	if message := mustFatal(t, func(tb testing.TB) { Load(tb, dir, 1) }); !strings.Contains(message, "would skip it silently") {
		t.Errorf("failure %q should say an unparseable file is not silently skipped", message)
	}
}

func TestCallsFailsWhenThePatternMatchesNothing(t *testing.T) {
	dir := writePackage(t, map[string]string{"a.go": "package p\n\nfunc f() {}\n"})
	pkg := Load(t, dir, 1)

	message := mustFatal(t, func(tb testing.TB) { pkg.Calls(tb, "safelog.SetLevel") })
	for _, needle := range []string{"no call to safelog.SetLevel", "pass vacuously", "rather than deleting it"} {
		if !strings.Contains(message, needle) {
			t.Errorf("failure %q does not carry %q", message, needle)
		}
	}
}

// CallsAllowingNone exists so a ban whose PASS condition is zero says so out loud instead
// of inheriting a floor it cannot satisfy.
func TestCallsAllowingNoneReturnsEmptyWithoutFailing(t *testing.T) {
	dir := writePackage(t, map[string]string{"a.go": "package p\n\nfunc f() {}\n"})
	pkg := Load(t, dir, 1)

	if sites := pkg.CallsAllowingNone("safelog.SetLevel"); len(sites) != 0 {
		t.Errorf("sites = %v, want none", sites)
	}
}

// Every match, not the first: finding one site per file is how a guard silently inspects
// less than its message claims.
func TestCallsFindsEveryMatchIncludingSeveralInOneFileAndFunction(t *testing.T) {
	dir := writePackage(t, map[string]string{
		"a.go": `package p

func first() {
	safelog.SetLevel(1)
	safelog.SetLevel(2)
}

func second() {
	safelog.SetLevel(3)
}
`,
		"b.go": `package p

func third() {
	safelog.SetLevel(4)
}
`,
	})
	pkg := Load(t, dir, 2)

	sites := pkg.Calls(t, "safelog.SetLevel")
	if len(sites) != 4 {
		t.Fatalf("found %d sites, want 4: %v", len(sites), sites)
	}
	wantFuncs := map[string]int{"first": 2, "second": 1, "third": 1}
	gotFuncs := map[string]int{}
	for _, site := range sites {
		gotFuncs[site.Func]++
		if site.Line == 0 || site.File == "" {
			t.Errorf("site %v carries no position; a census failure must name a place the reader can open", site)
		}
	}
	for name, want := range wantFuncs {
		if gotFuncs[name] != want {
			t.Errorf("%s holds %d sites, want %d — each site must be attributed to its enclosing function", name, gotFuncs[name], want)
		}
	}
}

// Contains is the check a file-scoped ban cannot make: the owning file legitimately holds
// the guarded call inside the owning function, and used to permit a stray one anywhere
// else in that same file.
func TestContainsScopesASiteToTheOwningFunctionNotTheFile(t *testing.T) {
	dir := writePackage(t, map[string]string{
		"a.go": `package p

func owner() {
	safelog.SetLevel(1)
}

func stray() {
	safelog.SetLevel(2)
}
`,
	})
	pkg := Load(t, dir, 1)
	fn, ok := pkg.Func("owner")
	if !ok {
		t.Fatal("owner not found")
	}

	inside, outside := 0, 0
	for _, site := range pkg.Calls(t, "safelog.SetLevel") {
		if pkg.Contains(fn, site) {
			inside++
			continue
		}
		outside++
	}
	if inside != 1 || outside != 1 {
		t.Errorf("inside = %d, outside = %d, want 1 and 1: a file-scoped check would report both as permitted", inside, outside)
	}
}

func TestFuncReportsNotFoundRatherThanPanicking(t *testing.T) {
	dir := writePackage(t, map[string]string{"a.go": "package p\n\nfunc f() {}\n"})
	pkg := Load(t, dir, 1)

	if _, ok := pkg.Func("missing"); ok {
		t.Error("Func reported a declaration that does not exist")
	}
	if fn, ok := pkg.Func("f"); !ok || fn.File != "a.go" || fn.Line == 0 {
		t.Errorf("Func(f) = %+v, %v; want it located with a real position", fn, ok)
	}
}

// A declaration is looked up across the package, so moving a helper to a sibling file is
// a refactor rather than a test failure — and the shape that used to panic on a -1 slice.
func TestFuncFindsADeclarationInAnySiblingFile(t *testing.T) {
	dir := writePackage(t, map[string]string{
		"a.go": "package p\n\nfunc caller() { owner() }\n",
		"b.go": "package p\n\nfunc owner() {}\n",
	})
	pkg := Load(t, dir, 2)

	fn, ok := pkg.Func("owner")
	if !ok {
		t.Fatal("owner not found in a sibling file; a census that reads one hardcoded file breaks on this refactor")
	}
	if fn.File != "b.go" {
		t.Errorf("owner located in %s, want b.go", fn.File)
	}
}

// Receiver-name-blind on purpose: a census keyed on one spelling is scoped to that
// spelling, which is how the identical assignment through a renamed variable survives.
func TestFieldAssignmentsIgnoresWhatTheReceiverIsCalled(t *testing.T) {
	dir := writePackage(t, map[string]string{
		"a.go": `package p

func f(cfg *C, other *C) {
	cfg.LogLevel = "warn"
	other.LogLevel = "error"
	cfg.Port = "1"
}
`,
	})
	pkg := Load(t, dir, 1)

	sites := pkg.FieldAssignments("LogLevel")
	if len(sites) != 2 {
		t.Fatalf("found %d LogLevel assignments, want 2 regardless of receiver name: %v", len(sites), sites)
	}
	if got := pkg.FieldAssignments("Port"); len(got) != 1 {
		t.Errorf("found %d Port assignments, want 1", len(got))
	}
}

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

// writeTree lays out files at paths relative to one root, creating parent directories, so
// a walk test can describe a whole shape in one literal.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func treeNames(files []SourceFile) []string {
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.Name)
	}
	return names
}

// The hazard this closes: a git worktree created INSIDE the repo — routine when copying a
// diff somewhere safe to mutate — is otherwise walked as module source. Its .git is a FILE,
// not a directory, so the obvious `IsDir() && name == ".git"` check misses exactly the case
// that occurs; and the nested directory's name is arbitrary, so no name list catches it.
func TestTreeSkipsANestedCheckoutWhicheverKindOfGitEntryItHas(t *testing.T) {
	// dir and gitEntry are separate because deriving the nested root from the .git path
	// puts the clone's sources INSIDE .git, where the name skip excludes them and the
	// subtest passes without ever testing a nested clone.
	for _, tc := range []struct {
		name     string
		dir      string
		gitEntry string
		body     string
	}{
		{name: "worktree carries a .git file", dir: "scratch-7f3a", gitEntry: ".git", body: "gitdir: /elsewhere/.git/worktrees/scratch\n"},
		{name: "clone carries a .git directory", dir: "vendored-copy", gitEntry: ".git/HEAD", body: "ref: refs/heads/main\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := writeTree(t, map[string]string{
				"real.go":                  "package a\n",
				"pkg/deep.go":              "package b\n",
				tc.dir + "/" + tc.gitEntry: tc.body,
				tc.dir + "/copy.go":        "package a\n",
				tc.dir + "/pkg/sub.go":     "package b\n",
			})

			// The nested sources must exist where the walk would find them, or this
			// subtest cannot distinguish an exclusion from a fixture that planted nothing.
			for _, rel := range []string{tc.dir + "/copy.go", tc.dir + "/pkg/sub.go"} {
				if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
					t.Fatalf("fixture did not plant %s, so the exclusion would pass vacuously: %v", rel, err)
				}
			}

			got := treeNames(Tree(t, root, 2))

			want := []string{"pkg/deep.go", "real.go"}
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("Tree = %v, want %v; a nested checkout's copies are walked as module source, and since rule tables are keyed module-relative, a copy of an exempt file cannot match its exemption", got, want)
			}
		})
	}
}

// The root itself holds a .git entry, so the skip cannot be unconditional or a census run
// from a real checkout enumerates nothing.
func TestTreeStillWalksARootThatIsItselfACheckout(t *testing.T) {
	root := writeTree(t, map[string]string{
		".git/HEAD":  "ref: refs/heads/main\n",
		"real.go":    "package a\n",
		"pkg/one.go": "package b\n",
	})

	if got := treeNames(Tree(t, root, 2)); len(got) != 2 {
		t.Errorf("Tree = %v, want both files; the root's own .git must not exclude the module", got)
	}
}

func TestTreeSkipsDependencyAndBuildTreesByName(t *testing.T) {
	root := writeTree(t, map[string]string{
		"real.go":               "package a\n",
		"pkg/one.go":            "package b\n",
		"node_modules/dep.go":   "package c\n",
		"dist/built.go":         "package d\n",
		"vendor/vendored.go":    "package e\n",
		"dist/nested/deeper.go": "package f\n",
	})

	got := treeNames(Tree(t, root, 2))

	want := []string{"pkg/one.go", "real.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Tree = %v, want %v", got, want)
	}
}

func TestTreeExcludesTestFilesAndNonGoFiles(t *testing.T) {
	root := writeTree(t, map[string]string{
		"real.go":      "package a\n",
		"pkg/one.go":   "package b\n",
		"real_test.go": "package a\n",
		"notes.md":     "# no\n",
		"pkg/two.json": "{}\n",
	})

	got := treeNames(Tree(t, root, 2))

	want := []string{"pkg/one.go", "real.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Tree = %v, want %v", got, want)
	}
}

// Tree carries Load's vacuity floor for the same reason: a walk that lost most of the tree
// — an exclusion widened by mistake, a root pointed one level too deep — must fail rather
// than report a clean census over almost nothing.
func TestTreeFailsBelowItsFileFloor(t *testing.T) {
	root := writeTree(t, map[string]string{"only.go": "package a\n"})

	got := mustFatal(t, func(tb testing.TB) { Tree(tb, root, 5) })

	for _, want := range []string{"walked 1 non-test files", "want at least 5", "pass vacuously"} {
		if !strings.Contains(got, want) {
			t.Errorf("Fatalf = %q, want it to mention %q", got, want)
		}
	}
}

// The file contents are what every module-wide census actually reads, so a walk returning
// names with empty bodies would let each one silently match nothing.
func TestTreeCarriesEachFilesTextAndAModuleRelativeName(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.go":            "package a\n// marker-alpha\n",
		"nested/pkg/b.go": "package b\n// marker-beta\n",
	})

	files := Tree(t, root, 2)
	if len(files) != 2 {
		t.Fatalf("Tree returned %d files, want 2", len(files))
	}
	if files[0].Name != "a.go" || !strings.Contains(files[0].Text, "marker-alpha") {
		t.Errorf("first file = %+v, want a.go carrying its text", files[0])
	}
	if files[1].Name != "nested/pkg/b.go" || !strings.Contains(files[1].Text, "marker-beta") {
		t.Errorf("second file = %+v, want a slash-separated relative name and its text", files[1])
	}
}

// TestTree exists because a rule ABOUT tests cannot be checked over the files Tree returns —
// it excludes exactly them. The point of it living here rather than as a bespoke walk in the
// package that needs it is that the exclusions are inherited, so both halves are asserted:
// the file class it selects, and the nested checkout it still skips.
func TestTestTreeSelectsTestFilesAndInheritsTheExclusions(t *testing.T) {
	root := writeTree(t, map[string]string{
		"keep_test.go":                      "package p\n",
		"nested/also_test.go":               "package p\n",
		"production.go":                     "package p\n",
		"notes.txt":                         "text\n",
		"scratch-copy/.git":                 "gitdir: /elsewhere\n",
		"scratch-copy/leaked_test.go":       "package p\n",
		"node_modules/dep/vendored_test.go": "package p\n",
	})

	var names []string
	for _, file := range TestTree(t, root, 2) {
		names = append(names, file.Name)
	}

	want := []string{"keep_test.go", "nested/also_test.go"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("TestTree = %v, want exactly %v — production sources and non-Go files are not its subject, and a nested checkout's copies are not module source", names, want)
	}
}

// The floor message has to name the class it counted, or a census that walked the wrong file
// class reads as a census that found too little.
func TestTestTreeFloorNamesTheFileClassItCounted(t *testing.T) {
	root := writeTree(t, map[string]string{"only_test.go": "package p\n"})

	got := mustFatal(t, func(tb testing.TB) { TestTree(tb, root, 5) })

	if !strings.Contains(got, "test files") {
		t.Errorf("floor failure said %q, want it to name the file class counted", got)
	}
}

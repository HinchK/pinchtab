// Package srccensus is the shared home for this repo's source-scanning census guards.
//
// A census guard pins something ordinary unit tests structurally cannot see: that
// production CALLS the guarded helper rather than reimplementing the rule inline. A unit
// test on the helper stays green while a caller bypasses it, which is the regression shape
// every one of these guards was written after.
//
// # The five properties
//
// Four rounds of these guards have now been written by hand, and the same two mistakes
// recur. Shared here, so they cannot be forgotten:
//
//  1. SCOPE IS DERIVED, NEVER HAND-LISTED. Load enumerates a package's non-test sources
//     rather than naming files, and the finders return EVERY match rather than the first.
//     Every silent narrowing found in this repo came from a hand-listed scope — a pair of
//     filenames, the first match per file, a literal list of the very constants whose
//     addition the guard policed. The rule was right in all three; the scope covered less
//     than the message claimed, and no vacuity floor can see that.
//  2. NON-VACUITY IS STRUCTURAL. Load fails when the package yields too few files, and
//     Calls fails when a pattern that must exist matches nothing. A guard whose scan
//     matched nothing passes for the wrong reason, so this is not left to each author.
//     Where zero matches is the PASS condition, CallsAllowingNone says so out loud and the
//     floor has to sit on something else.
//  3. LOCATING A DECLARATION EITHER SUCCEEDS OR REPORTS. Func returns found=false as a
//     value. The hand-rolled shape it replaces sliced from strings.Index and panicked with
//     a bounds error when a helper moved to a sibling file — the one refactor most likely
//     to cause it, answered with the least readable failure.
//  4. EXEMPTION TABLES STAY LOCAL, with the reason at the entry and checked in BOTH
//     directions: a stale exemption naming something that no longer exists must fail too.
//     They are deliberately not shared — the reason belongs beside the rule it excuses.
//  5. FAILURE MESSAGES STAY LOCAL AND SPECIFIC. Naming the file, the line and the fix is
//     most of a census's value, and saying what to do when the guarded shape CHANGED —
//     add the new entry point, or pin the property against whatever replaced it, but do
//     not delete the test — is what stops the next reader deleting it.
//
// This package deliberately holds no rule tables, no exemptions and no messages. It reads
// source with go/ast so a site carries its enclosing function and a real FileSet line; it
// does not decide what is banned.
package srccensus

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// Site is one match, positioned well enough to open and attributed to the function that
// contains it — the attribution a file-scoped scan cannot make.
type Site struct {
	File string
	Line int
	Func string
	Text string

	pos token.Pos
}

func (s Site) String() string {
	return fmt.Sprintf("%s:%d (in %s): %s", s.File, s.Line, s.Func, s.Text)
}

// Package is one Go package's non-test source, parsed.
type Package struct {
	fset  *token.FileSet
	files map[string]*ast.File
	dir   string
}

// Load parses every non-test .go file in dir. minFiles is the vacuity floor: pass the
// number of files the guard already knows about, not 1, so a scan that silently stops
// matching most of the package fails instead of passing.
func Load(t testing.TB, dir string, minFiles int) *Package {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("cannot enumerate %s, so this census would check nothing: %v", dir, err)
	}

	pkg := &Package{fset: token.NewFileSet(), files: map[string]*ast.File{}, dir: dir}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(pkg.fset, path, nil, 0)
		if err != nil {
			t.Fatalf("cannot parse %s, so this census would skip it silently: %v", path, err)
		}
		pkg.files[filepath.Base(path)] = file
	}
	if len(pkg.files) < minFiles {
		t.Fatalf("parsed %d non-test files in %s, want at least %d; the census matched almost nothing and would pass vacuously", len(pkg.files), dir, minFiles)
	}
	return pkg
}

// Files reports the base names parsed, for a guard that needs to name one.
func (p *Package) Files() []string {
	names := make([]string, 0, len(p.files))
	for name := range p.files {
		names = append(names, name)
	}
	return names
}

// Calls returns every call to want — either a bare "Fn" or a qualified "pkg.Fn" — and
// fails when there are none. Use it for a rule that says "this must happen HERE and
// nowhere else": a pattern that matches nothing means the rule has stopped applying to
// anything and the guard is guarding air.
func (p *Package) Calls(t testing.TB, want string) []Site {
	t.Helper()

	sites := p.CallsAllowingNone(want)
	if len(sites) == 0 {
		t.Fatalf("no call to %s anywhere in %s; the census has nothing to guard and would pass vacuously — re-point it at whatever replaced the call rather than deleting it", want, p.dir)
	}
	return sites
}

// CallsAllowingNone is Calls for a rule whose PASS condition is zero matches, such as a
// ban on a call that must not appear at all. The name is the point: a guard reading zero
// as success must say so, and must put its vacuity floor somewhere else.
func (p *Package) CallsAllowingNone(want string) []Site {
	var sites []Site
	p.each(func(name string, file *ast.File) {
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || calleeName(call.Fun) != want {
				return true
			}
			sites = append(sites, p.site(name, file, call, want))
			return true
		})
	})
	return sites
}

// FieldAssignments returns every assignment whose left-hand side is a selector ending in
// field — cfg.LogLevel = x, whatever the receiver is called. Receiver-name-blind, because
// a census keyed on one spelling is scoped to that spelling.
func (p *Package) FieldAssignments(field string) []Site {
	var sites []Site
	p.each(func(name string, file *ast.File) {
		ast.Inspect(file, func(node ast.Node) bool {
			assign, ok := node.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, lhs := range assign.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != field {
					continue
				}
				sites = append(sites, p.site(name, file, assign, "."+field+" ="))
			}
			return true
		})
	})
	return sites
}

// Func is a declaration found in the package, carrying the span a scope check needs.
type Func struct {
	Name string
	File string
	Line int

	start token.Pos
	end   token.Pos
}

// Func looks a declaration up ANYWHERE in the package and returns found=false rather than
// panicking or guessing. A guard reporting not-found should name the declaration and the
// directory it searched, so a helper that moved to a sibling file — or was renamed —
// explains itself instead of producing a slice-bounds error.
func (p *Package) Func(name string) (Func, bool) {
	var found Func
	ok := false
	p.each(func(fileName string, file *ast.File) {
		for _, decl := range file.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			if !isFunc || fn.Name.Name != name || ok {
				continue
			}
			position := p.fset.Position(fn.Pos())
			found = Func{Name: name, File: fileName, Line: position.Line, start: fn.Pos(), end: fn.End()}
			ok = true
		}
	})
	return found, ok
}

// Dir is the directory the census read, for a not-found message that says where it looked.
func (p *Package) Dir() string { return p.dir }

// Contains reports whether site lies inside this declaration's body. This is the check a
// file-scoped ban cannot make: a file that legitimately holds the guarded call inside the
// owning function also permitted a stray one anywhere else in the same file.
func (p *Package) Contains(fn Func, site Site) bool {
	// The site carries its exact position rather than being re-derived from its line: a
	// call sharing a line with the func declaration would otherwise read as outside it.
	return site.pos != token.NoPos && site.pos >= fn.start && site.pos < fn.end
}

func (p *Package) each(visit func(name string, file *ast.File)) {
	for _, name := range sortedKeys(p.files) {
		visit(name, p.files[name])
	}
}

func (p *Package) site(fileName string, file *ast.File, node ast.Node, text string) Site {
	position := p.fset.Position(node.Pos())
	return Site{File: fileName, Line: position.Line, Func: enclosingFunc(file, node.Pos()), Text: text, pos: node.Pos()}
}

func enclosingFunc(file *ast.File, pos token.Pos) string {
	name := "(file scope)"
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if pos >= fn.Pos() && pos < fn.End() {
			name = fn.Name.Name
		}
	}
	return name
}

func calleeName(fun ast.Expr) string {
	switch callee := fun.(type) {
	case *ast.Ident:
		return callee.Name
	case *ast.SelectorExpr:
		if pkg, ok := callee.X.(*ast.Ident); ok {
			return pkg.Name + "." + callee.Sel.Name
		}
		return callee.Sel.Name
	}
	return ""
}

func sortedKeys(files map[string]*ast.File) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return names
}

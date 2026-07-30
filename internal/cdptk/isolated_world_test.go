package cdptk

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/srccensus"
)

// moduleGoFileFloor is the vacuity floor for the module-wide walk below: a walk
// that stops seeing the tree would otherwise report a clean single world.
const moduleGoFileFloor = 400

type worldSite struct {
	file  string
	line  int
	value string
}

// Two isolated world names existed for one rule — a node scope here and a frame
// scope in the bridge — and nobody noticed the second arrive, which is the whole
// reason this census exists rather than a comment asking for one world.
//
// The rule is one world name in the whole module, named by a constant rather than
// spelled inline at the call, so a third cannot be added quietly: an inline
// literal at a second call site is exactly how the second one appeared.
func TestOnlyOneIsolatedWorldNameExists(t *testing.T) {
	var creates, names []worldSite

	for _, file := range srccensus.Tree(t, filepath.Join("..", ".."), moduleGoFileFloor) {
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file.Name, file.Text, 0)
		if err != nil {
			t.Errorf("%s: cannot parse, so this census can neither clear nor flag it: %v", file.Name, err)
			continue
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.BasicLit:
				if n.Kind == token.STRING && strings.Trim(n.Value, `"`) == "Page.createIsolatedWorld" {
					creates = append(creates, worldSite{file: file.Name, line: fset.Position(n.Pos()).Line})
				}
			case *ast.KeyValueExpr:
				key, ok := n.Key.(*ast.BasicLit)
				if !ok || key.Kind != token.STRING || strings.Trim(key.Value, `"`) != "worldName" {
					return true
				}
				site := worldSite{file: file.Name, line: fset.Position(n.Pos()).Line}
				if lit, inline := n.Value.(*ast.BasicLit); inline {
					site.value = "inline literal " + lit.Value
				} else if ident, named := n.Value.(*ast.Ident); named {
					site.value = ident.Name
				} else {
					site.value = "a computed expression"
				}
				names = append(names, site)
			}
			return true
		})
	}

	if len(creates) == 0 || len(names) == 0 {
		t.Fatalf("found %d Page.createIsolatedWorld call(s) and %d worldName argument(s) in the whole module; the census has nothing to guard and would pass vacuously — re-point it at whatever mints the isolated world now rather than deleting it", len(creates), len(names))
	}

	const why = "One world per frame is the rule: Page.createIsolatedWorld keys on frame and name, so a second name means two worlds in the same frame, and a handle from one is not usable in a Runtime.callFunctionOn with a handle from the other. Nothing in the code says which world a handle came from, and the failure is silent. Mint every isolated context through cdptk.IsolatedContextID, which takes the frame as a parameter."

	if len(creates) != 1 {
		t.Errorf("Page.createIsolatedWorld is called at %d sites (%v), want exactly one. %s", len(creates), describeWorldSites(creates), why)
	}
	if len(names) != 1 {
		t.Errorf("worldName is passed at %d sites (%v), want exactly one. %s", len(names), describeWorldSites(names), why)
	}
	for _, site := range names {
		if site.value != "isolatedWorldName" {
			t.Errorf("%s:%d passes worldName as %s rather than the isolatedWorldName constant; a name spelled at the call site is how the second world arrived. %s", site.file, site.line, site.value, why)
		}
	}
}

func describeWorldSites(sites []worldSite) []string {
	out := make([]string, 0, len(sites))
	for _, site := range sites {
		out = append(out, filepath.ToSlash(site.file)+":"+itoa(site.line))
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// The frame a caller names must be the frame the world is created for. Merging the
// two worlds is easiest to get wrong here: hand the owner an empty frame and every
// frame-scoped evaluation silently moves to the top frame's document, which reads
// as a selector that stopped matching rather than as a scope bug.
func TestTheNamedFrameReachesTheWorldCreation(t *testing.T) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "isolated_world.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	owner := findFuncDecl(parsed, "IsolatedContextID")
	if owner == nil {
		t.Fatal("IsolatedContextID is gone from isolated_world.go; re-point this guard at whatever mints the isolated world rather than deleting it")
	}
	param := owner.Type.Params.List[len(owner.Type.Params.List)-1].Names[0].Name

	found := false
	ast.Inspect(owner, func(node ast.Node) bool {
		kv, ok := node.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, isLit := kv.Key.(*ast.BasicLit)
		if !isLit || strings.Trim(key.Value, `"`) != "frameId" {
			return true
		}
		ident, isIdent := kv.Value.(*ast.Ident)
		if !isIdent || ident.Name != param {
			t.Errorf("frameId is passed as %s, not the %s parameter — the frame the caller asked for must be the frame the world is created for", exprText(kv.Value), param)
			return true
		}
		found = true
		return true
	})
	if !found {
		t.Errorf("no frameId argument in IsolatedContextID carries its %s parameter, so a caller naming a frame cannot be sure the world belongs to it", param)
	}
}

func findFuncDecl(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}

func exprText(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.BasicLit:
		return e.Value
	case *ast.CallExpr:
		return exprText(e.Fun) + "(...)"
	case *ast.SelectorExpr:
		return exprText(e.X) + "." + e.Sel.Name
	}
	return "a computed expression"
}

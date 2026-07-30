package testbrowser

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/pinchtab/pinchtab/internal/srccensus"
)

// The property the fix delivers, proven deterministically rather than sampled by
// re-running a suite: a browser profile whose removal FAILS must not fail the test.
// The subtest's own recorder stands in for a real Chrome still flushing its cache —
// on this platform a directory is unremovable while it is a mount point or read-only
// parent, so the honest simulation is to fail the removal by making the PARENT
// unwritable, which is exactly the class of error RemoveAll reports mid-flush.
func TestProfileDirCleanupToleratesAFailedRemoval(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "profile")
	if err := os.MkdirAll(filepath.Join(dir, "Default", "Cache", "Cache_Data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Default", "Cache", "Cache_Data", "index"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })

	recorder := &cleanupRecorder{TB: t}
	removeProfileDir(recorder, dir)

	if recorder.failed {
		t.Fatalf("a failed profile removal failed the test; that is the flake this helper exists to remove")
	}
	if !recorder.logged {
		t.Error("a failed profile removal was silent; the leak must be visible in the test log")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("the unremovable dir vanished, so this test did not exercise a failed removal: %v", err)
	}
}

func TestProfileDirRemovesTheDirectoryOnTheHappyPath(t *testing.T) {
	var dir string
	t.Run("inner", func(inner *testing.T) {
		dir = ProfileDir(inner)
		if err := os.WriteFile(filepath.Join(dir, "SingletonLock"), []byte("x"), 0o644); err != nil {
			inner.Fatal(err)
		}
	})
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("profile dir %s survived the test that created it (%v); the tolerance is for a racing browser, not a licence to leak", dir, err)
	}
}

// cleanupRecorder captures what removeProfileDir did to the test instead of doing it,
// so "does not fail" is an assertion rather than the absence of one.
type cleanupRecorder struct {
	testing.TB
	failed bool
	logged bool
}

func (r *cleanupRecorder) Helper()               {}
func (r *cleanupRecorder) Logf(string, ...any)   { r.logged = true }
func (r *cleanupRecorder) Errorf(string, ...any) { r.failed = true }
func (r *cleanupRecorder) Fatalf(string, ...any) { r.failed = true }
func (r *cleanupRecorder) Error(...any)          { r.failed = true }
func (r *cleanupRecorder) Fatal(...any)          { r.failed = true }
func (r *cleanupRecorder) Cleanup(f func())      { f() }

// A Chrome profile handed to t.TempDir is the defect itself: t.TempDir asserts its
// own RemoveAll succeeded, and a browser still flushing its cache makes that fail on
// an unrelated card's gate. The rule is checked over the whole module, because the
// next browser test is as likely to be written in a package that has none today.
// The subject is the tests themselves, so the enumeration comes from srccensus.TestTree
// rather than a walk written here: the nested-checkout exclusion is the part a bespoke walk
// silently loses, and it has one owner.
func TestNoBrowserProfileIsPointedAtATestTempDir(t *testing.T) {
	sites := 0

	for _, file := range srccensus.TestTree(t, "../..", moduleTestFileFloor) {
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), file.Path, file.Text, 0)
		if parseErr != nil {
			continue
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isSelectorCall(call.Fun, "chromedp", "UserDataDir") || len(call.Args) != 1 {
				return true
			}
			sites++
			if inner, ok := call.Args[0].(*ast.CallExpr); ok && isMethodCall(inner.Fun, "TempDir") {
				t.Errorf("%s hands a Chrome profile to t.TempDir; use testbrowser.ProfileDir(t) — t.TempDir fails the test when the browser is still flushing its cache", file.Name)
			}
			return true
		})
	}

	if sites < userDataDirSiteFloor {
		t.Fatalf("found only %d chromedp.UserDataDir call sites; the census matched almost nothing and would pass vacuously", sites)
	}
}

// Two floors, because they fail for different reasons: the TestTree floor catches a walk that
// stopped seeing files at all, and the site floor catches a rule that stopped matching what
// it polices. Both are set well under the real counts so ordinary growth or deletion does not
// trip them.
const (
	moduleTestFileFloor  = 200
	userDataDirSiteFloor = 20
)

func isSelectorCall(fun ast.Expr, pkg, name string) bool {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == pkg
}

func isMethodCall(fun ast.Expr, name string) bool {
	sel, ok := fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == name
}

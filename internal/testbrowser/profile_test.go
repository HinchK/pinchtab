package testbrowser

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
func TestNoBrowserProfileIsPointedAtATestTempDir(t *testing.T) {
	root := moduleRoot(t)
	sites := 0

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipCensusDir(path, root, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isSelectorCall(call.Fun, "chromedp", "UserDataDir") || len(call.Args) != 1 {
				return true
			}
			sites++
			if inner, ok := call.Args[0].(*ast.CallExpr); ok && isMethodCall(inner.Fun, "TempDir") {
				t.Errorf("%s hands a Chrome profile to t.TempDir; use testbrowser.ProfileDir(t) — t.TempDir fails the test when the browser is still flushing its cache", rel)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if sites < 20 {
		t.Fatalf("found only %d chromedp.UserDataDir call sites; the census matched almost nothing and would pass vacuously", sites)
	}
}

// skipCensusDir decides which directories the census must not descend into. A name list
// alone is not enough: a git worktree created inside the repo carries a .git FILE under an
// arbitrary name, so it looks like an ordinary directory while holding a second copy of
// every source file. The census would then report those copies as fresh violations, against
// module-relative paths that will vanish with the worktree — and on a shared tree that reds
// this package for whoever runs it next. The repo root carries .git too, so it is exempt.
func skipCensusDir(path, root, name string) bool {
	if name == ".git" || name == "node_modules" || name == "dist" {
		return true
	}
	if path == root {
		return false
	}
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

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

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's working directory; the census cannot find the module root")
		}
		dir = parent
	}
}

// The census walks from the module root, so anything that puts a SECOND copy of the tree
// inside the repo becomes a second set of findings. A git worktree does exactly that under
// an arbitrary name, carrying a .git FILE rather than a directory, so a name list cannot see
// it — and the violations it reports name paths that vanish with the worktree, which sends
// the reader to fix a file that was never the problem.
func TestCensusSkipsDirectoriesThatCarryTheirOwnGitMarker(t *testing.T) {
	root := t.TempDir()

	worktreeFile := filepath.Join(root, "wt-anything")
	if err := os.MkdirAll(worktreeFile, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreeFile, ".git"), []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	worktreeDir := filepath.Join(root, "clone-anything")
	if err := os.MkdirAll(filepath.Join(worktreeDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	ordinary := filepath.Join(root, "internal")
	if err := os.MkdirAll(ordinary, 0o755); err != nil {
		t.Fatal(err)
	}

	// The repo root carries .git too and must never be skipped, or the census covers nothing.
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		path string
		want bool
	}{
		{name: "a nested worktree, .git as a file", path: worktreeFile, want: true},
		{name: "a nested clone, .git as a directory", path: worktreeDir, want: true},
		{name: "an ordinary source directory", path: ordinary, want: false},
		{name: "the module root itself", path: root, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := skipCensusDir(tc.path, root, filepath.Base(tc.path)); got != tc.want {
				t.Errorf("skipCensusDir(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

//go:build !windows

package bridge

import "testing"

// cmdlineHasExactArg lives in cleanup.go, which is //go:build !windows. Keeping its
// test beside the tagged implementation is what stops `go vet ./...` and the package
// test binary from failing to build on Windows.
func TestCmdlineHasExactUserDataDirArgAvoidsPrefixCollision(t *testing.T) {
	args := [][]byte{
		[]byte("/Applications/Chrome"),
		[]byte("--user-data-dir=/var/lib/pt/profile10"),
		[]byte("--remote-debugging-port=9222"),
	}
	if cmdlineHasExactArg(args, "--user-data-dir=/var/lib/pt/profile1") {
		t.Fatal("profile1 matched profile10")
	}
	if !cmdlineHasExactArg(args, "--user-data-dir=/var/lib/pt/profile10") {
		t.Fatal("profile10 should match exact user-data-dir arg")
	}
}

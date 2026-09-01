//go:build !windows

package browserprobe

import "io/fs"

// IsExecutable reports whether a probed path is runnable. On unix that is the
// mode bits.
func IsExecutable(info fs.FileInfo, _ string) bool {
	return info.Mode()&0o111 != 0
}

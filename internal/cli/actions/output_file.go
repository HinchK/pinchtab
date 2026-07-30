package actions

import (
	"os"

	"github.com/pinchtab/pinchtab/internal/fileout"
)

// writeOutputFile writes buf to path and returns the path actually used.
//
// An auto-named path is reserved with an exclusive create, so a second run in the same
// second gets capture-<ts>-1.jpg rather than destroying the first file. The returned
// path is what the caller must print: it may carry a suffix the caller did not build,
// and printing the built name instead trades a silent overwrite for a confidently
// reported filename that does not exist.
//
// An explicit path is written as the user typed it, overwrite included — that is their
// instruction, not a collision.
func writeOutputFile(path string, autoNamed bool, buf []byte) (string, error) {
	if autoNamed {
		return fileout.WriteUniquePath(path, buf)
	}
	return path, os.WriteFile(path, buf, 0600)
}

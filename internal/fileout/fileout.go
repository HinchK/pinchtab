// Package fileout owns the no-overwrite rule for auto-named output files.
//
// Every auto-naming writer in this repo builds a name from a SECOND-resolution
// timestamp, so two outputs in the same second land on one path and the first is
// destroyed while the caller still reports that path and its byte count. Widening
// the timestamp only shrinks the window — two concurrent writers can share a
// millisecond — so the guarantee has to come from the create call itself.
//
// It lives here rather than in internal/handlers because the rule has two consumers
// that cannot import each other: the HTTP handlers and internal/cli/actions. A leaf
// depending only on the standard library is what both can reach. None of the existing
// shared packages owns filesystem output, and putting file creation in a sanitiser or
// an id generator would trade one small package for a responsibility violation.
//
// The boundary is generated default names only. A path the user typed is written as
// they typed it, overwrite included — that is their instruction, not a collision.
package fileout

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MaxUniqueNameAttempts bounds the suffix search. A directory holding this many files
// named for the same second is a runaway caller, not a collision to resolve.
const MaxUniqueNameAttempts = 512

// CreateUnique reserves a path under dir by CREATING it exclusively, bumping a numeric
// suffix until the create succeeds: base+ext, then base-1+ext, base-2+ext…
// It returns the open handle, so the name cannot be taken between the check and the
// write — which is the whole point, and why this is not a stat-then-write.
//
// The caller owns the base name, including its timestamp format, so this closes the
// collision without renaming anything a user might already be globbing for. The
// returned path is authoritative: it may carry a suffix the caller did not ask for,
// and reporting the name that was built instead trades a silent overwrite for a
// confidently printed filename that does not exist.
func CreateUnique(dir, base, ext string) (*os.File, string, error) {
	for attempt := 0; attempt < MaxUniqueNameAttempts; attempt++ {
		name := base + ext
		if attempt > 0 {
			name = fmt.Sprintf("%s-%d%s", base, attempt, ext)
		}
		path := filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			return f, path, nil
		}
		if !os.IsExist(err) {
			return nil, "", err
		}
	}
	return nil, "", fmt.Errorf("could not find a free name for %s%s in %s after %d attempts", base, ext, dir, MaxUniqueNameAttempts)
}

// WriteUnique creates a fresh file under dir and writes buf to it, returning the path
// actually used.
func WriteUnique(dir, base, ext string, buf []byte) (string, error) {
	f, path, err := CreateUnique(dir, base, ext)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(buf); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return path, nil
}

// WriteUniquePath is WriteUnique for a caller holding a whole path rather than its
// three parts, which is the shape every CLI site has.
func WriteUniquePath(path string, buf []byte) (string, error) {
	dir, base, ext := splitPath(path)
	return WriteUnique(dir, base, ext, buf)
}

// ReservePath claims a path for a caller that will not write the bytes itself — one
// moving an already-written file into place with os.Rename. The placeholder is created
// exclusively and closed, and the rename then replaces it atomically on POSIX, so the
// reservation is what stops a second process taking the name in between.
//
// A reservation is a side effect: every path that then gives up must Remove the
// returned path, or an abandoned run litters an empty file under a name that reads
// like a real output.
func ReservePath(path string) (string, error) {
	dir, base, ext := splitPath(path)
	f, reserved, err := CreateUnique(dir, base, ext)
	if err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(reserved)
		return "", err
	}
	return reserved, nil
}

// splitPath cuts a path into the three parts CreateUnique numbers, so the suffix lands
// before the extension (capture-<ts>-1.jpg) rather than after it.
func splitPath(path string) (dir, base, ext string) {
	dir = filepath.Dir(path)
	name := filepath.Base(path)
	ext = filepath.Ext(name)
	return dir, strings.TrimSuffix(name, ext), ext
}

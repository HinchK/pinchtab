package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// imageContentType maps an image format to its MIME type (jpeg default).
func imageContentType(format string) string {
	if format == "png" {
		return "image/png"
	}
	return "image/jpeg"
}

// imageExt maps an image format to its file extension (.jpg default).
func imageExt(format string) string {
	if format == "png" {
		return ".png"
	}
	return ".jpg"
}

// writeRawImage writes raw binary output with the given content type, logging
// (but not surfacing) a write error under logLabel.
func writeRawImage(w http.ResponseWriter, buf []byte, contentType, logLabel string) {
	w.Header().Set("Content-Type", contentType)
	if _, err := w.Write(buf); err != nil {
		slog.Error(logLabel, "err", err)
	}
}

// maxUniqueNameAttempts bounds the suffix search. A directory holding this many
// files named for the same second is a runaway caller, not a collision to resolve.
const maxUniqueNameAttempts = 512

// createUniqueFile reserves a path under dir by CREATING it exclusively, bumping a
// numeric suffix until the create succeeds: base+ext, then base-1+ext, base-2+ext…
// It returns the open handle, so the name cannot be taken between the check and the
// write — which is the whole point, and why this is not a stat-then-write.
//
// Every server-side auto-naming writer goes through here. They all built a name from a
// SECOND-resolution timestamp and then wrote it unconditionally, so two outputs in the
// same second landed on one path: the first file was destroyed while its response still
// reported that path, its byte count and its clip rect. Widening the timestamp only
// shrinks the window — two concurrent requests can share a millisecond — so the
// guarantee has to come from the create call itself.
//
// The caller owns the base name, including its timestamp format, so this closes the
// collision without renaming anything a user might already be globbing for.
func createUniqueFile(dir, base, ext string) (*os.File, string, error) {
	for attempt := 0; attempt < maxUniqueNameAttempts; attempt++ {
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
	return nil, "", fmt.Errorf("could not find a free name for %s%s in %s after %d attempts", base, ext, dir, maxUniqueNameAttempts)
}

// writeUniqueFile creates a fresh file under dir and writes buf to it, returning the
// path actually used — which may carry a suffix the caller did not ask for.
func writeUniqueFile(dir, base, ext string, buf []byte) (string, error) {
	f, path, err := createUniqueFile(dir, base, ext)
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

// exportTimestamp is the second-resolution stamp every auto-named export shares. It is
// no longer load-bearing for uniqueness — createUniqueFile is — so it stays at second
// granularity to keep filenames readable.
func exportTimestamp() string {
	return time.Now().Format("20060102-150405")
}

// saveBinaryToStateDir writes buf to StateDir/<subdir>/<prefix>-<ts><ext> using
// the standard binary-export modes (dir 0750, file 0600) and returns the path
// and timestamp. This is the single persistence policy shared by the PDF,
// screenshot, and capture handlers' default-location output.
//
// The returned path is authoritative: on a same-second collision it carries a suffix,
// so callers must report this value rather than rebuilding the name from the timestamp.
func saveBinaryToStateDir(stateDir, subdir, prefix, ext string, buf []byte) (filePath, timestamp string, err error) {
	dir := filepath.Join(stateDir, subdir)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", "", err
	}
	timestamp = exportTimestamp()
	filePath, err = writeUniqueFile(dir, prefix+"-"+timestamp, ext, buf)
	if err != nil {
		return "", "", err
	}
	return filePath, timestamp, nil
}

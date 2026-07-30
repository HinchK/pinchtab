package fileout

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Two writes of the same auto-built name must land on different paths with both files
// intact. The name comes from a second-resolution timestamp, so without the exclusive
// create the second write destroys the first.
func TestWritingTheSameNameTwiceKeepsBothFiles(t *testing.T) {
	dir := t.TempDir()
	first := []byte("first payload, deliberately longer than the second")
	second := []byte("second")

	firstPath, err := WriteUnique(dir, "capture-20260101-120000", ".jpg", first)
	if err != nil {
		t.Fatal(err)
	}
	secondPath, err := WriteUnique(dir, "capture-20260101-120000", ".jpg", second)
	if err != nil {
		t.Fatal(err)
	}

	if firstPath == secondPath {
		t.Fatalf("both writes landed on %s; the first file was destroyed", firstPath)
	}
	if got := filepath.Base(secondPath); got != "capture-20260101-120000-1.jpg" {
		t.Errorf("second path = %s, want the suffix before the extension", got)
	}
	assertContents(t, firstPath, first)
	assertContents(t, secondPath, second)
}

// The suffix goes before the extension so the file is still recognised by its type. A
// naive append would produce capture-<ts>.jpg-1, which no image viewer opens.
func TestTheSuffixLandsBeforeTheExtension(t *testing.T) {
	dir := t.TempDir()
	for i, want := range []string{"page-x.pdf", "page-x-1.pdf", "page-x-2.pdf"} {
		got, err := WriteUniquePath(filepath.Join(dir, "page-x.pdf"), []byte("pdf"))
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(got) != want {
			t.Errorf("write %d landed on %s, want %s", i, filepath.Base(got), want)
		}
	}
}

// The handle is returned open on purpose: a stat-then-create leaves a window in which a
// second caller takes the name between the check and the write.
func TestTheReservationHoldsTheNameWhileTheFirstHandleIsStillOpen(t *testing.T) {
	dir := t.TempDir()

	first, firstPath, err := CreateUnique(dir, "rec", ".gif")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()

	second, secondPath, err := CreateUnique(dir, "rec", ".gif")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()

	if firstPath == secondPath {
		t.Fatalf("both reservations returned %s while the first handle was still open", firstPath)
	}
}

// Exhaustion is a refusal, not a fallback to overwriting: a directory holding this many
// files for one second is a runaway caller.
func TestAFullDirectoryIsRefusedRatherThanOverwritten(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < MaxUniqueNameAttempts; i++ {
		name := "full.bin"
		if i > 0 {
			name = strings.Replace(name, ".bin", "-"+strconv.Itoa(i)+".bin", 1)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := CreateUnique(dir, "full", ".bin"); err == nil {
		t.Fatal("CreateUnique succeeded with every name taken; it must refuse rather than overwrite")
	}
}

// ReservePath exists for a caller that renames an already-written file into place. The
// placeholder must hold the name against a second reserver, and a rename over it must
// still land the real bytes.
func TestReservePathHoldsTheNameAndSurvivesARenameOverIt(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "recording-20260101-120000.gif")

	firstReserved, err := ReservePath(target)
	if err != nil {
		t.Fatal(err)
	}
	secondReserved, err := ReservePath(target)
	if err != nil {
		t.Fatal(err)
	}
	if firstReserved == secondReserved {
		t.Fatalf("both reservations returned %s; the placeholder did not hold the name", firstReserved)
	}

	source := filepath.Join(dir, "server-encoded.gif")
	payload := []byte("the encoded recording")
	if err := os.WriteFile(source, payload, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(source, firstReserved); err != nil {
		t.Fatalf("rename over our own reservation failed: %v", err)
	}
	assertContents(t, firstReserved, payload)
}

func assertContents(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path) // #nosec G304 -- path produced by this test.
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("%s holds %q, want %q", path, got, want)
	}
}

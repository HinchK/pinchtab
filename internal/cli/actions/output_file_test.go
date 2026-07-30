package actions

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Every auto-named CLI output builds its name from a second-resolution timestamp, so
// two runs in one second used to land on one path and the first file was destroyed —
// silently, while the command printed that path as if it held the new bytes. Driven
// through the real commands rather than the helper, because the defect was as much in
// what each site PRINTS as in what it writes.
func TestTwoRunsInOneSecondKeepBothFilesAndPrintTheNameEachUsed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prefix string
		ext    string
		run    func(t *testing.T) string
	}{
		{
			name:   "capture",
			prefix: "capture-",
			ext:    ".jpg",
			run: func(t *testing.T) string {
				m := newMockServer()
				defer m.close()
				m.response = `{"status":"ok","image":{"format":"jpeg","base64":"aW1n"}}`
				cmd := captureCmd()
				return captureStdout(t, func() { Capture(m.server.Client(), m.base(), "", cmd) })
			},
		},
		{
			name:   "pdf",
			prefix: "page-",
			ext:    ".pdf",
			run: func(t *testing.T) string {
				m := newMockServer()
				defer m.close()
				m.response = "%PDF-1.4 fake"
				cmd := pdfCmd()
				return captureStdout(t, func() { PDF(m.server.Client(), m.base(), "", cmd) })
			},
		},
		{
			name:   "screenshot",
			prefix: "screenshot-",
			ext:    ".jpg",
			run: func(t *testing.T) string {
				m := newMockServer()
				defer m.close()
				m.response = "rawimagebytes"
				cmd := screenshotCmd()
				return captureStdout(t, func() { Screenshot(m.server.Client(), m.base(), "", cmd) })
			},
		},
		{
			name:   "screenshot --annotate",
			prefix: "screenshot-",
			ext:    ".jpg",
			run: func(t *testing.T) string {
				m := newMockServer()
				defer m.close()
				m.response = `{"format":"jpeg","base64":"aW1n","annotations":[]}`
				cmd := screenshotCmd()
				_ = cmd.Flags().Set("annotate", "true")
				return captureStdout(t, func() { Screenshot(m.server.Client(), m.base(), "", cmd) })
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := chdirTemp(t)

			firstOut := tc.run(t)
			secondOut := tc.run(t)

			written := autoNamedFiles(t, dir, tc.prefix, tc.ext)
			if len(written) != 2 {
				t.Fatalf("two runs left %v; the second overwrote the first", written)
			}
			for _, name := range written {
				if info, err := os.Stat(filepath.Join(dir, name)); err != nil || info.Size() == 0 {
					t.Errorf("%s is missing or empty: %v", name, err)
				}
			}

			// Each run must print the path it actually used, so the two printed names are
			// distinct and both are on disk. Printing the name each run BUILT would have
			// them print one identical name — the file the second run did not write.
			printed := []string{
				printedName(t, firstOut, tc.prefix, tc.ext),
				printedName(t, secondOut, tc.prefix, tc.ext),
			}
			if printed[0] == printed[1] {
				t.Fatalf("both runs printed %s, but two different files were written (%v)", printed[0], written)
			}
			onDisk := map[string]bool{written[0]: true, written[1]: true}
			var suffixed int
			for i, name := range printed {
				if !onDisk[name] {
					t.Errorf("run %d printed %s, which is not one of the files written (%v)", i, name, written)
				}
				if strings.HasSuffix(strings.TrimSuffix(name, tc.ext), "-1") {
					suffixed++
				}
			}
			if suffixed != 1 {
				t.Errorf("printed names %v carry %d collision suffixes, want exactly one", printed, suffixed)
			}
		})
	}
}

// The boundary the parent card pinned: a path the user typed is written as they typed
// it. Overwriting there is their instruction, not a collision to resolve — and a
// suffixed name would break a script that goes on to read the path it passed.
func TestAnExplicitOutputPathStillOverwrites(t *testing.T) {
	dir := chdirTemp(t)
	target := filepath.Join(dir, "chosen.jpg")
	if err := os.WriteFile(target, []byte("stale bytes from an earlier run"), 0600); err != nil {
		t.Fatal(err)
	}

	m := newMockServer()
	defer m.close()
	m.response = `{"status":"ok","image":{"format":"jpeg","base64":"aW1n"}}`
	cmd := captureCmd()
	_ = cmd.Flags().Set("output", target)

	captureStdout(t, func() { Capture(m.server.Client(), m.base(), "", cmd) })

	got, err := os.ReadFile(target) // #nosec G304 -- path built by this test.
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "img" {
		t.Errorf("explicit path holds %q, want the new bytes", got)
	}
	if extra := autoNamedFiles(t, dir, "chosen", ".jpg"); len(extra) != 1 {
		t.Errorf("explicit path produced %v; it must not gain a suffix", extra)
	}
}

// The census: every auto-naming site in this package must reach the exclusive create.
// A survivor is either a caller-supplied path or named here as deliberate, so a sixth
// site cannot land silently the way these five did.
func TestNoCLIActionAutoNamesAFileItThenOverwrites(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	// file -> the call that reserves the name it builds.
	accounted := map[string]string{
		"actions_capture.go":    "writeOutputFile with autoNamed",
		"actions_pdf.go":        "writeOutputFile with autoNamed",
		"actions_screenshot.go": "writeOutputFile with autoNamed",
		"actions_record.go":     "fileout.ReservePath before the rename",
	}

	var scanned, withTimestamp int
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(name) // #nosec G304 -- files listed from this package's own directory.
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		if !strings.Contains(string(body), "150405") {
			continue
		}
		withTimestamp++
		if accounted[name] == "" {
			t.Errorf("%s builds a name from a second-resolution timestamp and is not accounted for; route it through writeOutputFile or fileout, or record why it cannot collide", name)
		}
	}

	if scanned < len(accounted) {
		t.Fatalf("scanned %d files in this package; this census would pass vacuously", scanned)
	}
	if withTimestamp != len(accounted) {
		t.Errorf("found %d auto-naming files but %d are accounted for; a stale entry hides a site that no longer reserves", withTimestamp, len(accounted))
	}
	for name := range accounted {
		if _, err := os.Stat(name); err != nil {
			t.Errorf("accounted file %s no longer exists; the census is guarding nothing", name)
		}
	}
}

// printedName pulls the output filename out of whatever sentence a site wraps it in —
// each of the four phrases it differently, and the assertion is about the name.
func printedName(t *testing.T, out, prefix, ext string) string {
	t.Helper()
	for _, field := range strings.Fields(out) {
		// record prints an absolute path, the other four a bare name; the assertion is
		// about which file was named, so compare the basename either way.
		if name := filepath.Base(field); strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ext) {
			return name
		}
	}
	t.Fatalf("output %q names no %s*%s file", strings.TrimSpace(out), prefix, ext)
	return ""
}

func chdirTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	// t.TempDir can hand back a symlinked path (/var vs /private/var on darwin), which
	// would make the printed path and the listed name disagree for reasons unrelated to
	// the guard.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func autoNamedFiles(t *testing.T, dir, prefix, ext string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) && strings.HasSuffix(entry.Name(), ext) {
			names = append(names, entry.Name())
		}
	}
	return names
}

func captureCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("output", "", "")
	cmd.Flags().Bool("json", false, "")
	cmd.Flags().String("format", "", "")
	cmd.Flags().String("quality", "", "")
	cmd.Flags().String("selector", "", "")
	cmd.Flags().String("filter", "", "")
	cmd.Flags().String("depth", "", "")
	cmd.Flags().String("wait", "", "")
	cmd.Flags().Bool("beyond-viewport", false, "")
	cmd.Flags().String("scale", "", "")
	cmd.Flags().Bool("require-pair", false, "")
	cmd.Flags().Bool("with-bounds", true, "")
	cmd.Flags().String("tab", "", "")
	return cmd
}

func pdfCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("output", "", "")
	cmd.Flags().String("tab", "", "")
	cmd.Flags().Bool("landscape", false, "")
	cmd.Flags().String("scale", "", "")
	cmd.Flags().String("paper-width", "", "")
	cmd.Flags().String("paper-height", "", "")
	cmd.Flags().String("margin-top", "", "")
	cmd.Flags().String("margin-bottom", "", "")
	cmd.Flags().String("margin-left", "", "")
	cmd.Flags().String("margin-right", "", "")
	cmd.Flags().String("page-ranges", "", "")
	cmd.Flags().Bool("prefer-css-page-size", false, "")
	cmd.Flags().Bool("display-header-footer", false, "")
	cmd.Flags().String("header-template", "", "")
	cmd.Flags().String("footer-template", "", "")
	cmd.Flags().Bool("generate-tagged-pdf", false, "")
	cmd.Flags().Bool("generate-document-outline", false, "")
	cmd.Flags().Bool("file-output", false, "")
	cmd.Flags().String("path", "", "")
	return cmd
}

func screenshotCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("output", "", "")
	cmd.Flags().Bool("annotate", false, "")
	cmd.Flags().String("format", "", "")
	cmd.Flags().String("quality", "", "")
	cmd.Flags().String("selector", "", "")
	cmd.Flags().String("scale", "", "")
	cmd.Flags().Bool("beyond-viewport", false, "")
	cmd.Flags().String("tab", "", "")
	return cmd
}

// The record site cannot use the writer form: the bytes are already on disk under the
// server's name, so it reserves and renames. Two stops in one second must therefore
// still keep both recordings and print the name each one took.
func TestTwoRecordStopsInOneSecondKeepBothRecordings(t *testing.T) {
	dir := chdirTemp(t)
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))

	stop := func(encoded string) string {
		serverPath := filepath.Join(dir, encoded)
		if err := os.WriteFile(serverPath, []byte("encoded "+encoded), 0600); err != nil {
			t.Fatal(err)
		}
		m := newMockServer()
		defer m.close()
		m.setResponse("POST", "/record/stop", 200, `{"status":"ok","path":"`+serverPath+`","frames":3}`)
		m.setResponse("GET", "/record/status", 200, `{"state":"finished"}`)
		return captureStdout(t, func() { RecordStop(m.server.Client(), m.base(), "") })
	}

	firstOut := stop("srv-a.gif")
	secondOut := stop("srv-b.gif")

	written := autoNamedFiles(t, dir, "recording-", ".gif")
	if len(written) != 2 {
		t.Fatalf("two stops left %v; the second rename destroyed the first recording", written)
	}
	for _, name := range written {
		assertNonEmpty(t, filepath.Join(dir, name))
	}

	first := printedName(t, firstOut, "recording-", ".gif")
	second := printedName(t, secondOut, "recording-", ".gif")
	if first == second {
		t.Fatalf("both stops reported %s, but two files were written (%v)", first, written)
	}
	if !strings.HasSuffix(strings.TrimSuffix(second, ".gif"), "-1") {
		t.Errorf("second stop reported %s, want the collision suffix it actually used", second)
	}
}

// A reservation is a side effect: the name is claimed before the rename, so a rename
// that fails must release it rather than leave an empty file wearing an output's name —
// zero bytes under recording-<ts>.gif reads as a recording that was made.
//
// Driven in a child process because the failure path is cli.Fatal, which calls os.Exit.
func TestAFailedRenameReleasesTheReservation(t *testing.T) {
	if dir := os.Getenv(recordFatalChildEnv); dir != "" {
		runFailedRenameChild(dir)
		return
	}

	dir := t.TempDir()
	child := exec.Command(os.Args[0], "-test.run=TestAFailedRenameReleasesTheReservation", "-test.timeout=60s") // #nosec G204 -- re-executes this test binary.
	child.Env = append(os.Environ(), recordFatalChildEnv+"="+dir)
	raw, err := child.CombinedOutput()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("child exited with %v, want the failed rename's os.Exit(1); output:\n%s", err, raw)
	}
	if left := autoNamedFiles(t, dir, "recording-", ".gif"); len(left) != 0 {
		t.Errorf("the failed stop left %v behind; an abandoned reservation is an empty file wearing an output's name", left)
	}
}

const recordFatalChildEnv = "PINCHTAB_TEST_RECORD_FATAL_DIR"

func runFailedRenameChild(dir string) {
	if err := os.Chdir(dir); err != nil {
		panic(err)
	}
	if err := os.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state")); err != nil {
		panic(err)
	}

	missing := filepath.Join(dir, "never-encoded.gif")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/record/stop" {
			_, _ = w.Write([]byte(`{"status":"ok","path":"` + missing + `","frames":3}`))
			return
		}
		_, _ = w.Write([]byte(`{"state":"finished"}`))
	}))
	defer srv.Close()

	RecordStop(srv.Client(), srv.URL, "")
}

func assertNonEmpty(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Errorf("%s is empty", path)
	}
}

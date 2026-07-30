// profile_lock.go handles stale browser profile lock recovery for
// Chromium-based providers.
//
// When a container restarts (or the browser crashes), Chromium's
// SingletonLock, SingletonSocket, and SingletonCookie files may be left behind
// in the profile directory. On next startup the browser sees these and refuses
// to launch with
// "The profile appears to be in use by another Chromium process".
//
// This code detects that error, checks whether the owning process is actually
// still running (via PID probe and process listing), and removes the stale
// lock files if it's safe to do so. It retries browser startup once after
// clearing the locks.

package bridge

import (
	"bytes"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var chromeProfileProcessLister = findChromeProfileProcesses
var chromePIDIsRunning = isChromePIDRunning
var killChromeProfileProcesses = killProcesses
var isProfileOwnedByRunningPinchtabMock = isProfileOwnedByRunningPinchtab
var isPinchTabProcessFunc = isPinchTabProcess

// lockStartupGrace is the window after a PID file is written during which the
// browser process check is skipped. AcquireProfileLock writes the PID before
// InitBrowser launches the browser, so a second process must not treat the
// missing browser as a stale lock during this startup window.
var lockStartupGrace = 2 * time.Minute

var chromeSingletonFiles = []string{
	"SingletonLock",
	"SingletonSocket",
	"SingletonCookie",
}

var chromeProfileLockPIDPattern = regexp.MustCompile(`(?:Chromium|Chrome) process \((\d+)\)`)

type chromeProfileProcess struct {
	PID     string
	Command string
}

func isProfileLockError(msg string) bool {
	if msg == "" {
		return false
	}
	return strings.Contains(msg, "The profile appears to be in use by another Chromium process") ||
		strings.Contains(msg, "The profile appears to be in use by another Chrome process") ||
		strings.Contains(msg, "process_singleton")
}

func clearStaleProfileLocks(profileDir, errMsg string) (bool, error) {
	if strings.TrimSpace(profileDir) == "" {
		return false, nil
	}

	if pid, ok := extractChromeProfileLockPID(errMsg); ok {
		running, err := chromePIDIsRunning(pid)
		if err != nil {
			slog.Warn("failed to probe browser lock pid; falling back to process listing", "profile", profileDir, "pid", pid, "err", err)
		} else if running {
			if owned, ptPid := isProfileOwnedByRunningPinchtabMock(profileDir); owned {
				slog.Warn("browser profile lock appears active and owned by another pinchtab; leaving singleton files in place", "profile", profileDir, "pid", pid, "pinchtab_pid", ptPid)
				return false, nil
			}
			slog.Warn("browser profile lock appears active but pinchtab owner is dead; proceeding with stale cleanup", "profile", profileDir, "pid", pid)
		}
	}

	processes, err := chromeProfileProcessLister(profileDir)
	if err != nil {
		if _, ok := extractChromeProfileLockPID(errMsg); ok {
			slog.Warn("profile process listing unavailable; proceeding with stale lock cleanup based on lock pid", "profile", profileDir, "err", err)
		} else {
			return false, err
		}
	}
	if len(processes) > 0 {
		if owned, ptPid := isProfileOwnedByRunningPinchtabMock(profileDir); owned {
			pids := make([]string, 0, len(processes))
			for _, proc := range processes {
				pids = append(pids, proc.PID)
			}
			slog.Warn("browser profile lock appears active and owned by another pinchtab; leaving singleton files in place", "profile", profileDir, "pids", strings.Join(pids, ","), "pinchtab_pid", ptPid)
			return false, nil
		}

		slog.Warn("browser profile lock appears active but no pinchtab owner found; killing stale processes", "profile", profileDir)
		if err := killChromeProfileProcesses(processes); err != nil {
			slog.Error("failed to kill stale browser processes", "profile", profileDir, "err", err)
			return false, nil
		}
	}

	removed := false
	for _, name := range chromeSingletonFiles {
		path := filepath.Join(profileDir, name)
		if _, err := os.Lstat(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return false, fmt.Errorf("inspect %s: %w", path, err)
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return removed, fmt.Errorf("remove %s: %w", path, err)
		}
		removed = true
	}

	return removed, nil
}

// quarantineExitWait bounds how long quarantine waits for the dying browser
// to release the profile before renaming. Var so tests can shrink it.
var quarantineExitWait = 5 * time.Second

const quarantineSuffix = ".quarantine-"

var quarantineDirName = regexp.MustCompile(`^(.+)` + regexp.QuoteMeta(quarantineSuffix) + `(\d+)$`)

// SplitQuarantineName reads a quarantine directory name back into the profile it was
// made from and the timestamp it carries. It is the one owner of the pattern: the
// predicate below and the prune both go through it, so a reader cannot drift from what
// quarantine writes.
func SplitQuarantineName(dirName string) (profile string, stamp int64, ok bool) {
	match := quarantineDirName.FindStringSubmatch(dirName)
	if match == nil {
		return "", 0, false
	}
	stamp, err := strconv.ParseInt(match[2], 10, 64)
	if err != nil {
		return "", 0, false
	}
	return match[1], stamp, true
}

// IsQuarantinedProfileDir reports whether a directory name is one quarantine
// produced. Only the "<name>.quarantine-<unix>" suffix decides, so a profile a
// user named after the word stays an ordinary profile.
func IsQuarantinedProfileDir(dirName string) bool {
	_, _, ok := SplitQuarantineName(dirName)
	return ok
}

// QuarantineRemoval is one directory a prune reclaimed, for the caller to log and
// for a user-invoked reclaim to report.
type QuarantineRemoval struct {
	Path  string
	Bytes int64
}

// KeepAllQuarantinedProfiles is the keep count that turns pruning off, restoring
// the behaviour of keeping every quarantined profile for ever.
const KeepAllQuarantinedProfiles = 0

// PruneQuarantinedProfiles keeps the newest `keep` quarantined siblings of one
// profile and removes the rest, returning what it reclaimed. It is the only
// deleter of quarantined profiles, so a user-invoked reclaim reuses it rather
// than growing a second one.
//
// Two things bound what it can touch. Eligibility goes through
// IsQuarantinedProfileDir, the predicate quarantine's own writer uses, so a live
// profile directory sitting beside them is not a candidate — and because the
// pattern demands the "<name>.quarantine-<digits>" suffix, a profile a user named
// after the word is not one either. A user who names a profile exactly
// "<something>.quarantine-1700000000" IS indistinguishable on disk from a real
// quarantine; what stops it being deleted is the sibling scope, since it would
// have to sit in the profiles base dir under the name of ANOTHER profile with
// that suffix, and it is still never the newest-kept entry of its own name.
//
// justCreated is excluded by path, not by being newest: quarantine may proceed
// while a dying browser still holds the directory, so the entry that can still be
// written to must never be a candidate even if a same-second timestamp ties it
// with an older one.
func PruneQuarantinedProfiles(profileDir, justCreated string, keep int) ([]QuarantineRemoval, error) {
	if keep <= KeepAllQuarantinedProfiles {
		return nil, nil
	}
	profileDir = strings.TrimSpace(profileDir)
	if profileDir == "" {
		return nil, nil
	}

	siblings, err := quarantinedSiblings(profileDir)
	if err != nil {
		return nil, err
	}
	// Newest first, so keeping a prefix keeps the freshest artefacts — the ones
	// most likely to relate to a problem being investigated now.
	sort.Slice(siblings, func(i, j int) bool { return siblings[i].stamp > siblings[j].stamp })

	// The just-created directory takes the first slot before anything else is
	// considered, so keep=1 means exactly one quarantined profile survives and it is
	// that one. Reserving rather than ranking is what makes this independent of the
	// timestamp order: two quarantines in the same second tie, and the entry a dying
	// browser may still be writing to must not lose a coin toss.
	budget := keep
	for _, sibling := range siblings {
		if sibling.path == justCreated {
			budget--
			break
		}
	}

	var removals []QuarantineRemoval
	for _, sibling := range siblings {
		if sibling.path == justCreated {
			continue
		}
		if budget > 0 {
			budget--
			continue
		}
		reclaimed := dirBytes(sibling.path)
		if err := os.RemoveAll(sibling.path); err != nil {
			slog.Warn("could not prune quarantined profile", "profile", sibling.path, "err", err)
			continue
		}
		removals = append(removals, QuarantineRemoval{Path: sibling.path, Bytes: reclaimed})
	}
	return removals, nil
}

type quarantinedSibling struct {
	path  string
	stamp int64
}

// quarantinedSiblings finds the quarantined directories belonging to one profile: same
// parent, same profile name. Both halves have exactly one owner — SplitQuarantineName
// decides whether a name is a quarantine at all, and the profile it reports decides whose
// it is, which is what keeps one profile's prune away from another profile's evidence.
func quarantinedSiblings(profileDir string) ([]quarantinedSibling, error) {
	parent := filepath.Dir(profileDir)
	profileName := filepath.Base(profileDir)
	entries, err := os.ReadDir(parent)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read profiles dir: %w", err)
	}

	var siblings []quarantinedSibling
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		profile, stamp, ok := SplitQuarantineName(entry.Name())
		if !ok {
			continue
		}
		if profile != profileName {
			continue
		}
		siblings = append(siblings, quarantinedSibling{path: filepath.Join(parent, entry.Name()), stamp: stamp})
	}
	return siblings, nil
}

func dirBytes(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if info, err := entry.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// quarantineCorruptedProfile renames profileDir to "<profileDir>.quarantine-<ts>"
// and recreates an empty directory at the original path. Used to recover
// from silent CDP attach failures where CloakBrowser refuses to ingest
// existing profile state.
//
// keep bounds how many quarantined siblings of this profile survive, newest first,
// pruned here rather than on a startup sweep: a sweep would delete directories the
// operator never asked about, while this only reclaims as the same profile keeps
// failing. Directories belonging to profiles that never quarantine again are
// therefore never reclaimed by this path.
func quarantineCorruptedProfile(profileDir string, keep int) (string, error) {
	profileDir = strings.TrimSpace(profileDir)
	if profileDir == "" {
		return "", fmt.Errorf("empty profile dir")
	}
	if _, err := os.Stat(profileDir); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("stat profile dir: %w", err)
	}
	// The caller cancels chromedp contexts without waiting for the process to
	// exit; renaming under a still-running browser fails on Windows and lets
	// the old process keep writing into the quarantined dir on POSIX. On
	// timeout, proceed anyway — refusing entirely would block recovery.
	if !waitForChromeExit(profileDir, quarantineExitWait) {
		slog.Warn("quarantining profile while a browser process may still hold it", "profile", profileDir)
	}
	quarantinePath := fmt.Sprintf("%s%s%d", profileDir, quarantineSuffix, time.Now().Unix())
	if err := os.Rename(profileDir, quarantinePath); err != nil {
		return "", fmt.Errorf("rename profile dir: %w", err)
	}
	// The rename carries profile.json along, where it now names a profile this
	// directory no longer is. Drop it so nothing on disk makes that claim.
	if err := os.Remove(filepath.Join(quarantinePath, "profile.json")); err != nil && !os.IsNotExist(err) {
		slog.Warn("stale profile metadata left in quarantined profile", "profile", quarantinePath, "err", err)
	}
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		return quarantinePath, fmt.Errorf("recreate profile dir: %w", err)
	}
	// After the rename, so a failed quarantine never costs an older artefact.
	removals, err := PruneQuarantinedProfiles(profileDir, quarantinePath, keep)
	if err != nil {
		slog.Warn("could not prune older quarantined profiles", "profile", profileDir, "err", err)
	}
	for _, removal := range removals {
		slog.Info("pruned older quarantined profile", "profile", removal.Path, "bytesReclaimed", removal.Bytes, "keep", keep)
	}
	return quarantinePath, nil
}

func isProfileOwnedByRunningPinchtab(profileDir string) (bool, int) {
	pidFile := filepath.Join(profileDir, "pinchtab.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Debug("failed to read pinchtab pid file", "path", pidFile, "err", err)
		}
		return false, 0
	}

	var pid int
	if _, err := fmt.Sscanf(string(data), "%d", &pid); err != nil {
		slog.Debug("failed to parse pinchtab pid file", "path", pidFile, "err", err)
		return false, 0
	}

	if pid == os.Getpid() {
		return false, pid // It's us
	}

	running, err := chromePIDIsRunning(pid)
	if err == nil && running {
		// Even if the PID is running, check if it's actually a pinchtab process
		// to handle PID reuse.
		if isPinchTabProcessFunc(pid) {
			// Skip the browser check while the PID file is fresh: AcquireProfileLock
			// writes the file before InitBrowser launches the browser, so a second
			// process must not steal the lock during that startup window.
			if info, statErr := os.Stat(pidFile); statErr == nil && time.Since(info.ModTime()) < lockStartupGrace {
				slog.Debug("profile lock belongs to a recently started pinchtab; treating as active",
					"profile", profileDir, "pid", pid)
				return true, pid
			}
			processes, procErr := chromeProfileProcessLister(profileDir)
			if procErr == nil && len(processes) == 0 {
				slog.Debug("pinchtab pid file points to a running pinchtab but no browser is using the profile; treating lock as stale",
					"profile", profileDir, "pid", pid)
				return false, 0
			}
			if procErr != nil {
				slog.Debug("unable to verify browser ownership for running pinchtab pid; keeping profile locked",
					"profile", profileDir, "pid", pid, "err", procErr)
			}
			slog.Debug("profile is owned by another active pinchtab", "profile", profileDir, "pid", pid)
			return true, pid
		}
		slog.Debug("PID in lockfile is running but not a pinchtab process (PID reuse)", "profile", profileDir, "pid", pid)
	} else {
		slog.Debug("PID in lockfile is not running", "profile", profileDir, "pid", pid, "err", err)
	}

	return false, 0
}

func AcquireProfileLock(profileDir string) error {
	if profileDir == "" {
		return nil
	}
	if err := os.MkdirAll(profileDir, 0755); err != nil {
		return fmt.Errorf("mkdir profile dir: %w", err)
	}

	if owned, pid := isProfileOwnedByRunningPinchtab(profileDir); owned {
		return fmt.Errorf("profile %s is already in use by pinchtab process %d", profileDir, pid)
	}

	pidFile := filepath.Join(profileDir, "pinchtab.pid")
	slog.Debug("acquiring profile lock", "profile", profileDir, "pid", os.Getpid())
	return os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
}

func extractChromeProfileLockPID(msg string) (int, bool) {
	if msg == "" {
		return 0, false
	}
	match := chromeProfileLockPIDPattern.FindStringSubmatch(msg)
	if len(match) != 2 {
		return 0, false
	}
	pid := 0
	for _, ch := range match[1] {
		pid = pid*10 + int(ch-'0')
	}
	if pid <= 0 {
		return 0, false
	}
	return pid, true
}

func findChromeProfileProcesses(profileDir string) ([]chromeProfileProcess, error) {
	if strings.TrimSpace(profileDir) == "" {
		return nil, nil
	}

	cmd := exec.Command("ps", "-axo", "pid=,args=")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list chrome processes: %w", err)
	}

	return parseChromeProfileProcesses(out, profileDir), nil
}

func parseChromeProfileProcesses(out []byte, profileDir string) []chromeProfileProcess {
	if len(out) == 0 || strings.TrimSpace(profileDir) == "" {
		return nil
	}

	needleEquals := "--user-data-dir=" + profileDir
	needleSpace := "--user-data-dir " + profileDir
	lines := bytes.Split(out, []byte{'\n'})
	processes := make([]chromeProfileProcess, 0)

	for _, rawLine := range lines {
		line := strings.TrimSpace(string(rawLine))
		if line == "" || (!strings.Contains(line, needleEquals) && !strings.Contains(line, needleSpace)) {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		processes = append(processes, chromeProfileProcess{
			PID:     fields[0],
			Command: strings.TrimSpace(strings.TrimPrefix(line, fields[0])),
		})
	}

	return processes
}

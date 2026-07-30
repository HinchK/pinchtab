package bridge

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func writeProfileDir(t *testing.T, path string, bytes int) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if bytes > 0 {
		if err := os.WriteFile(filepath.Join(path, "State"), make([]byte, bytes), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func dirNames(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

func quarantinePathAt(profileDir string, stamp int64) string {
	return fmt.Sprintf("%s%s%d", profileDir, quarantineSuffix, stamp)
}

// The policy: keep the newest quarantined copy of a profile and prune its older
// siblings when a new one is created. Nothing in the product reads a quarantined
// profile, so the freshest is the only one with a plausible use.
func TestPruneKeepsTheNewestQuarantinedSibling(t *testing.T) {
	root := t.TempDir()
	profileDir := filepath.Join(root, "default")
	writeProfileDir(t, profileDir, 0)
	for _, stamp := range []int64{1700000001, 1700000002, 1700000003} {
		writeProfileDir(t, quarantinePathAt(profileDir, stamp), 64)
	}
	newest := quarantinePathAt(profileDir, 1700000004)
	writeProfileDir(t, newest, 8)

	removals, err := PruneQuarantinedProfiles(profileDir, newest, 1)
	if err != nil {
		t.Fatalf("PruneQuarantinedProfiles() error = %v", err)
	}
	if len(removals) != 3 {
		t.Fatalf("removals = %v, want the three older siblings", removals)
	}
	for _, removal := range removals {
		if removal.Bytes != 64 {
			t.Errorf("%s reported %d bytes reclaimed, want the 64 it held", removal.Path, removal.Bytes)
		}
	}
	if got := dirNames(t, root); len(got) != 2 || got[0] != "default" || got[1] != filepath.Base(newest) {
		t.Errorf("profiles dir = %v, want the live profile and the newest quarantine only", got)
	}
}

// keep=2 keeps the just-created one plus the next newest, so the setting means what
// it says rather than "one plus keep".
func TestPruneKeepCountIncludesTheJustCreatedDirectory(t *testing.T) {
	root := t.TempDir()
	profileDir := filepath.Join(root, "default")
	writeProfileDir(t, profileDir, 0)
	for _, stamp := range []int64{1700000001, 1700000002} {
		writeProfileDir(t, quarantinePathAt(profileDir, stamp), 16)
	}
	newest := quarantinePathAt(profileDir, 1700000003)
	writeProfileDir(t, newest, 16)

	removals, err := PruneQuarantinedProfiles(profileDir, newest, 2)
	if err != nil {
		t.Fatalf("PruneQuarantinedProfiles() error = %v", err)
	}
	if len(removals) != 1 || filepath.Base(removals[0].Path) != filepath.Base(quarantinePathAt(profileDir, 1700000001)) {
		t.Fatalf("removals = %v, want only the oldest", removals)
	}
}

// Quarantine may proceed while a dying browser still holds the directory, so the
// just-created entry is the one that can still be written to. It is excluded by path,
// not by being newest: two quarantines in the same second tie on the timestamp.
func TestPruneNeverRemovesTheJustCreatedDirectoryEvenOnATimestampTie(t *testing.T) {
	root := t.TempDir()
	profileDir := filepath.Join(root, "default")
	writeProfileDir(t, profileDir, 0)
	justCreated := quarantinePathAt(profileDir, 1700000009)
	writeProfileDir(t, justCreated, 4)
	// A sibling that sorts equal-or-newer, which would otherwise spend the only slot.
	tie := quarantinePathAt(profileDir, 1700000009) + "0"
	writeProfileDir(t, tie, 4)

	removals, err := PruneQuarantinedProfiles(profileDir, justCreated, 1)
	if err != nil {
		t.Fatalf("PruneQuarantinedProfiles() error = %v", err)
	}
	if len(removals) != 1 || removals[0].Path != tie {
		t.Fatalf("removals = %v, want the other sibling and never the just-created one", removals)
	}
	if _, err := os.Stat(justCreated); err != nil {
		t.Fatalf("the just-created quarantine was removed: %v", err)
	}
}

// Only quarantine-suffixed siblings of the SAME profile are eligible: a live profile
// standing beside them, and another profile's quarantines, are both out of reach.
func TestPruneLeavesLiveProfilesAndOtherProfilesQuarantinesAlone(t *testing.T) {
	root := t.TempDir()
	profileDir := filepath.Join(root, "default")
	writeProfileDir(t, profileDir, 32)
	writeProfileDir(t, filepath.Join(root, "work"), 32)
	writeProfileDir(t, quarantinePathAt(filepath.Join(root, "work"), 1700000001), 32)
	writeProfileDir(t, filepath.Join(root, "default.quarantine-notanumber"), 32)
	writeProfileDir(t, filepath.Join(root, "default-quarantine-1700000001"), 32)
	writeProfileDir(t, quarantinePathAt(profileDir, 1700000001), 32)
	newest := quarantinePathAt(profileDir, 1700000002)
	writeProfileDir(t, newest, 32)

	removals, err := PruneQuarantinedProfiles(profileDir, newest, 1)
	if err != nil {
		t.Fatalf("PruneQuarantinedProfiles() error = %v", err)
	}
	if len(removals) != 1 || removals[0].Path != quarantinePathAt(profileDir, 1700000001) {
		t.Fatalf("removals = %v, want only this profile's older quarantine", removals)
	}
	for _, name := range []string{"default", "work", "work.quarantine-1700000001", "default.quarantine-notanumber", "default-quarantine-1700000001"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("%s was removed and must not have been: %v", name, err)
		}
	}
}

// Eligibility comes from the predicate quarantine's writer uses, not a second copy of
// the pattern: every name this prune accepts must be one that predicate accepts.
func TestPruneEligibilityAgreesWithTheQuarantinePredicate(t *testing.T) {
	root := t.TempDir()
	profileDir := filepath.Join(root, "default")
	writeProfileDir(t, profileDir, 0)
	for _, name := range []string{
		"default.quarantine-1700000001",
		"default.quarantine-notanumber",
		"default-quarantine-1700000002",
		"default.quarantine-",
	} {
		writeProfileDir(t, filepath.Join(root, name), 8)
	}

	siblings, err := quarantinedSiblings(profileDir)
	if err != nil {
		t.Fatalf("quarantinedSiblings() error = %v", err)
	}
	if len(siblings) != 1 {
		t.Fatalf("siblings = %v, want only the well-formed quarantine name", siblings)
	}
	for _, sibling := range siblings {
		if !IsQuarantinedProfileDir(filepath.Base(sibling.path)) {
			t.Errorf("%s is a prune candidate the quarantine predicate rejects", sibling.path)
		}
	}
}

// Keep-everything is the way back to the old behaviour, and it has to be exact: no
// directory is touched and nothing is reported as reclaimed.
func TestPruneKeepAllRemovesNothing(t *testing.T) {
	root := t.TempDir()
	profileDir := filepath.Join(root, "default")
	writeProfileDir(t, profileDir, 0)
	for _, stamp := range []int64{1700000001, 1700000002, 1700000003} {
		writeProfileDir(t, quarantinePathAt(profileDir, stamp), 8)
	}
	before := dirNames(t, root)

	removals, err := PruneQuarantinedProfiles(profileDir, "", KeepAllQuarantinedProfiles)
	if err != nil {
		t.Fatalf("PruneQuarantinedProfiles() error = %v", err)
	}
	if len(removals) != 0 {
		t.Fatalf("removals = %v, want none when every quarantine is kept", removals)
	}
	if got := dirNames(t, root); strings.Join(got, ",") != strings.Join(before, ",") {
		t.Errorf("profiles dir = %v, want it unchanged at %v", got, before)
	}
}

// The whole point is that pruning happens when a new quarantine is created, not on a
// sweep — so it has to be reachable through the quarantine path itself.
func TestQuarantineCorruptedProfilePrunesOlderSiblings(t *testing.T) {
	root := t.TempDir()
	profileDir := filepath.Join(root, "default")
	writeProfileDir(t, profileDir, 16)
	for _, stamp := range []int64{1700000001, 1700000002} {
		writeProfileDir(t, quarantinePathAt(profileDir, stamp), 16)
	}

	quarantinePath, err := quarantineCorruptedProfile(profileDir, 1)
	if err != nil {
		t.Fatalf("quarantineCorruptedProfile() error = %v", err)
	}
	if quarantinePath == "" {
		t.Fatal("no quarantine path returned")
	}

	got := dirNames(t, root)
	if len(got) != 2 {
		t.Fatalf("profiles dir = %v, want the recreated profile and the new quarantine only", got)
	}
	if _, err := os.Stat(quarantinePath); err != nil {
		t.Errorf("the new quarantine is gone: %v", err)
	}
	if _, err := os.Stat(profileDir); err != nil {
		t.Errorf("the profile directory was not recreated: %v", err)
	}
}

// Keeping everything through the quarantine path leaves exactly what was there plus
// the new directory, which is what "restores today's behaviour" has to mean.
func TestQuarantineCorruptedProfileKeepsEverythingWhenConfigured(t *testing.T) {
	root := t.TempDir()
	profileDir := filepath.Join(root, "default")
	writeProfileDir(t, profileDir, 16)
	for _, stamp := range []int64{1700000001, 1700000002} {
		writeProfileDir(t, quarantinePathAt(profileDir, stamp), 16)
	}

	if _, err := quarantineCorruptedProfile(profileDir, KeepAllQuarantinedProfiles); err != nil {
		t.Fatalf("quarantineCorruptedProfile() error = %v", err)
	}
	if got := dirNames(t, root); len(got) != 4 {
		t.Fatalf("profiles dir = %v, want both older quarantines kept alongside the new one", got)
	}
}

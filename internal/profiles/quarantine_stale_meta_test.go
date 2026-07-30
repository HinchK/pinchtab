package profiles

import (
	"os"
	"path/filepath"
	"testing"
)

// quarantineCarryingStaleMeta builds the on-disk state quarantine leaves when the
// profile.json removal does not succeed: a quarantined directory still holding metadata
// that names the live profile. Quarantine renames the directory and only WARNS if it
// cannot drop the file afterwards, and it deliberately proceeds while a dying browser may
// still hold the directory, so this state is reachable rather than hypothetical.
func quarantineCarryingStaleMeta(t *testing.T, baseDir, name string) (liveDir, quarantineDir string) {
	t.Helper()

	id := profileID(name)
	liveDir = writeListableProfile(t, baseDir, id)
	if err := writeProfileMeta(liveDir, ProfileMeta{ID: id, Name: name}); err != nil {
		t.Fatal(err)
	}

	quarantineDir = writeListableProfile(t, baseDir, id+".quarantine-1700000001")
	if err := writeProfileMeta(quarantineDir, ProfileMeta{ID: id, Name: name}); err != nil {
		t.Fatal(err)
	}
	return liveDir, quarantineDir
}

// The invariant trustedProfileMeta exists for: a quarantined directory must never answer
// to the live profile's identity. Its profile.json still says otherwise, and ProfileID
// hashes the name, so trusting that file would give two directories one name AND one ID —
// after which a lookup, a listing row or a DELETE by id can land on either. Nothing here
// tests the removal that normally prevents the state; the point is that the identity rules
// hold even when it has not happened.
func TestAQuarantineCarryingStaleMetadataNeverClaimsTheLiveProfileIdentity(t *testing.T) {
	baseDir := t.TempDir()
	liveDir, quarantineDir := quarantineCarryingStaleMeta(t, baseDir, "shopping")
	pm := NewProfileManager(baseDir)

	// This one is defended twice, and only one defence is designed: a quarantine name is
	// the live directory's name plus a suffix, so ReadDir yields the live directory first
	// and the id match wins before the metadata is ever consulted. Measured — weakening
	// trustedProfileMeta leaves this subtest passing. The identity subtest below is what
	// actually holds the line, so do not read a green here as covering the rule. The same
	// applies to the delete subtest: Delete resolves through findProfileDirByName, so it
	// inherits this lookup's answer and also passes under the weakening. Both are here for
	// the consequences they describe, not as evidence for the rule.
	t.Run("a lookup by name resolves the live directory", func(t *testing.T) {
		got, err := pm.findProfileDirByName("shopping")
		if err != nil {
			t.Fatalf("findProfileDirByName() error = %v", err)
		}
		if got != liveDir {
			t.Errorf("resolved %q, want the live directory %q — resolving the quarantine would point every later write at evidence", got, liveDir)
		}
	})

	t.Run("the two directories keep distinct names and ids", func(t *testing.T) {
		listed, err := pm.List()
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(listed) != 2 {
			t.Fatalf("listed %d profiles, want the live one and its quarantine", len(listed))
		}

		names := map[string]int{}
		ids := map[string]int{}
		for _, profile := range listed {
			names[profile.Name]++
			ids[profile.ID]++
		}
		for name, count := range names {
			if count > 1 {
				t.Errorf("%d listed profiles share the name %q; the quarantine is claiming the live profile's identity", count, name)
			}
		}
		for id, count := range ids {
			if count > 1 {
				t.Errorf("%d listed profiles share the id %q, so a DELETE by id can reach either directory", count, id)
			}
		}
	})

	t.Run("deleting the profile by name leaves the quarantine on disk", func(t *testing.T) {
		if err := pm.Delete("shopping"); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
		if _, err := os.Stat(liveDir); !os.IsNotExist(err) {
			t.Errorf("live directory still present after Delete: %v", err)
		}
		if _, err := os.Stat(quarantineDir); err != nil {
			t.Errorf("Delete removed the quarantined directory instead of, or as well as, the profile: %v", err)
		}
		if _, err := os.Stat(filepath.Join(quarantineDir, "Default")); err != nil {
			t.Errorf("the quarantined directory lost its contents: %v", err)
		}
	})
}

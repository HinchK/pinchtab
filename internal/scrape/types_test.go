package scrape

import "testing"

// The literal here is deliberately a second copy of SchemaVersion, and it is the
// point of the test. Every other assertion compares the report's field to the
// constant that stamped it, so it can prove the report is stamped but never that
// the version is the intended one — a bump passes the whole Go suite and surfaces
// only in the Docker e2e, long after it lands.
//
// This does not check correctness. It forces a bump to be acknowledged, together
// with the sibling artefacts that carry the same number.
func TestSchemaVersionIsPinnedSoABumpMustBeAcknowledged(t *testing.T) {
	const pinned = "2.0"

	if SchemaVersion != pinned {
		t.Fatalf("scrape SchemaVersion = %q, pinned at %q.\n"+
			"A scrape schema bump is a breaking change for report consumers. If it is intended, update all three:\n"+
			"  1. this literal,\n"+
			"  2. the schema history in docs/scrape.md — say which fields changed, as 1.0 -> 2.0 did for totalDiscovered -> totalURLsInSitemap,\n"+
			"  3. EXPECTED_SCRAPE_SCHEMA in tests/e2e/scenarios/cli/scrape-basic.sh.",
			SchemaVersion, pinned)
	}
}

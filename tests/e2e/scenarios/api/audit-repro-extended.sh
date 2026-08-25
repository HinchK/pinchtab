#!/bin/bash
# audit-repro-extended.sh — the audit reproducibility guarantee: the same
# input produces the same report, whatever concurrency it ran at. Two crawls,
# one serial and one concurrent, carry every assertion here. Volatile fields
# are stripped by fixtures/audit-site/normalize-report.jq, whose header owns
# the rationale for each one and the golden-report.json regeneration recipe.

GROUP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${GROUP_DIR}/../../helpers/api.sh"

NORMALIZE="${GROUP_DIR}/../../fixtures/audit-site/normalize-report.jq"
GOLDEN="${GROUP_DIR}/../../fixtures/audit-site/golden-report.json"
SITEMAP="${FIXTURES_URL}/audit-site/sitemap.xml"
ARTIFACT_DIR="${E2E_ARTIFACT_DIR:-/results}"
ARTIFACT_PREFIX="${ARTIFACT_DIR}/audit-repro-${PINCHTAB_E2E_BROWSER:-chrome}"

audit_body() {
  printf '{"sitemapUrl":"%s","options":{"screenshot":false},"concurrency":%s}' "$SITEMAP" "$1"
}

# ─────────────────────────────────────────────────────────────────
start_test "two audit runs normalize byte-identical"

pt_post /audit -d "$(audit_body 1)"
assert_ok "serial audit run"
SERIAL_RAW="$RESULT"
RUN_ONE=$(echo "$RESULT" | jq -S -f "$NORMALIZE")

pt_post /audit -d "$(audit_body 4)"
assert_ok "concurrent audit run"
CONCURRENT_RAW="$RESULT"
RUN_TWO=$(echo "$RESULT" | jq -S -f "$NORMALIZE")

# The one field the normalizer collapses because it legitimately differs here,
# so each run still has to prove it echoes the concurrency it was asked for.
assert_json_eq "$SERIAL_RAW" '.options.concurrency' "1" "serial run reports concurrency 1"
assert_json_eq "$CONCURRENT_RAW" '.options.concurrency' "4" "concurrent run reports concurrency 4"

# Matrix runs previously overwrote the next provider's report, losing the one
# normalized payload needed to diagnose a provider-only golden mismatch.
mkdir -p "$ARTIFACT_DIR"
printf '%s\n' "$RUN_ONE" > "${ARTIFACT_PREFIX}-live.json"

if [ -n "$RUN_ONE" ] && [ "$RUN_ONE" = "$RUN_TWO" ]; then
  pass_assert "normalized reports are byte-identical"
else
  fail_assert "normalized reports differ"
  diff <(echo "$RUN_ONE") <(echo "$RUN_TWO") | head -20
fi

end_test

# ─────────────────────────────────────────────────────────────────
start_test "normalized report matches the checked-in golden"

if [ ! -f "$GOLDEN" ]; then
  fail_assert "golden report exists at $GOLDEN"
else
  # Cloak's jsErrors count is structurally 0 (see the normalize filter header),
  # so it is dropped from both sides for that provider only.
  GOLDEN_CMP=$(cat "$GOLDEN")
  LIVE_CMP="$RUN_ONE"
  if [ "${PINCHTAB_E2E_BROWSER:-chrome}" = "cloak" ]; then
    LIVE_CMP=$(echo "$RUN_ONE" | jq -S 'del(.pages[].browser.jsErrors?)')
    GOLDEN_CMP=$(echo "$GOLDEN_CMP" | jq -S 'del(.pages[].browser.jsErrors?)')
  fi

  if diff -q <(echo "$LIVE_CMP") <(echo "$GOLDEN_CMP") >/dev/null 2>&1; then
    pass_assert "live report matches golden-report.json"
  else
    fail_assert "live report diverges from golden-report.json (schema/content drift; see normalize-report.jq header for how to regenerate)"
    diff -u <(echo "$GOLDEN_CMP") <(echo "$LIVE_CMP") > "${ARTIFACT_PREFIX}-golden.diff" || true
    head -30 "${ARTIFACT_PREFIX}-golden.diff"
  fi
fi

end_test

# ─────────────────────────────────────────────────────────────────
start_test "concurrency 4 audits the same page set as serial"

SET_ONE=$(echo "$RUN_ONE" | jq -S '[.pages[].url] | sort')
SET_FOUR=$(echo "$RUN_TWO" | jq -S '[.pages[].url] | sort')

if [ "$SET_ONE" = "$SET_FOUR" ]; then
  pass_assert "identical page URL set at concurrency 1 and 4"
else
  fail_assert "page sets differ: $SET_ONE vs $SET_FOUR"
fi

end_test

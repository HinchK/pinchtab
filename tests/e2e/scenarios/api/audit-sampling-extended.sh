#!/bin/bash
# audit-sampling-extended.sh — POST /audit template-group sampling.

GROUP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${GROUP_DIR}/../../helpers/api.sh"

SAMPLING_BODY="{\"sitemapUrl\":\"${FIXTURES_URL}/audit-site/sitemap.xml\",\"sampleSize\":2,\"options\":{\"screenshot\":false}}"

# ─────────────────────────────────────────────────────────────────
start_test "sampleSize=2 keeps all non-group pages plus 2 products"

pt_post /audit -d "$SAMPLING_BODY"
assert_ok "sampled sitemap audit"

assert_json_length "$RESULT" '.pages' 9 "9 pages (7 non-group + 2 sampled products)"

PRODUCT_COUNT=$(echo "$RESULT" | jq '[.pages[].url | select(contains("products/"))] | length')
if [ "$PRODUCT_COUNT" -eq 2 ]; then
  pass_assert "exactly 2 products/* pages"
else
  fail_assert "exactly 2 products/* pages (got: $PRODUCT_COUNT)"
fi

for page in index.html broken-assets.html console-errors.html a11y-issues.html clean.html forms.html cookie-echo.html; do
  if echo "$RESULT" | jq -e --arg page "$page" '.pages[] | select(.url | endswith($page))' >/dev/null 2>&1; then
    pass_assert "non-group page $page present"
  else
    fail_assert "non-group page $page present"
  fi
done

assert_json_contains "$RESULT" '.pages[0].url' "index.html" "homepage first in page order"

end_test

# ─────────────────────────────────────────────────────────────────
# Stability is pinned against the checked-in expectation rather than against a
# second crawl: the plan is a pure function of the sitemap (SamplePages keeps
# every ungrouped page in sitemap order, then the lexically-first sampleSize
# members of each template group), so naming the pages the sampler must choose
# also catches a selection that is stable but wrong — which comparing two runs
# of the same build never can.
start_test "sampling picks the documented pages on every run"

SAMPLED=$(echo "$RESULT" | jq -c '[.pages[].url | split("/") | last] | sort')
EXPECTED='["a11y-issues.html","broken-assets.html","clean.html","console-errors.html","cookie-echo.html","forms.html","index.html","p1.html","p2.html"]'

if [ "$SAMPLED" = "$EXPECTED" ]; then
  pass_assert "sampled page set is the 7 non-group pages plus the lexically-first 2 products"
else
  fail_assert "sampled page set drifted: $SAMPLED, want $EXPECTED"
fi

end_test

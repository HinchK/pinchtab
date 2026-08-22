#!/bin/bash

GROUP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${GROUP_DIR}/../../helpers/api.sh"

start_test "tab budget: tab creation stays fast under fingerprint-rotate churn"

URL="${FIXTURES_URL}/index.html"
BODY="$(mktemp)"
timeouts=0
slow=0
total_navs=0

nav_once() {
  local started dur
  started=$(get_time_ms)
  e2e_curl -s -o "$BODY" -w '%{http_code}' \
    -X POST "${E2E_SERVER}/navigate" -H 'Content-Type: application/json' \
    -d "{\"url\":\"${URL}\"}" >/dev/null
  dur=$(($(get_time_ms) - started))
  total_navs=$((total_navs + 1))
  if grep -q 'did not open a new tab' "$BODY" 2>/dev/null; then
    timeouts=$((timeouts + 1))
  elif [ "$dur" -ge 8000 ]; then
    slow=$((slow + 1))
  fi
}

nav_once

for round in 1 2 3; do
  pt_post /fingerprint/rotate '{"os":"windows"}' >/dev/null 2>&1 || true
  for n in 1 2 3 4 5 6; do nav_once; done
done

rm -f "$BODY"
echo -e "  ${MUTED}navigations=${total_navs} create-timeouts=${timeouts} slow(>=8s)=${slow}${NC}"

if [ "$timeouts" -eq 0 ]; then
  pass_assert "no create-tab timeouts across ${total_navs} navigations"
else
  fail_assert "${timeouts}/${total_navs} navigations hit the 10s create-tab timeout"
fi

if [ "$slow" -eq 0 ]; then
  pass_assert "no navigation stalled >= 8s"
else
  fail_assert "${slow}/${total_navs} navigations stalled >= 8s"
fi

end_test

#!/bin/bash
# browser-activity-extended.sh — route metadata on the activity EVENT, which only
# happens in bridge mode: the front door records the request in one process and the
# instance builds the route in another, so the two never meet there. This needs a
# second server in the stack, which is why it is not in the PR tier beside
# browser-activity-basic.sh.

GROUP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${GROUP_DIR}/../../helpers/api.sh"

EXPECTED_BROWSER="${PINCHTAB_E2E_BROWSER:-chrome}"
ACTIVITY_TIMEOUT="${E2E_ACTIVITY_TIMEOUT:-10}"

assert_route_metadata() {
  local route="$1" label="$2"
  local used escalated attempts_len last_accepted

  used=$(echo "$route" | jq -r '.usedProvider // empty')
  escalated=$(echo "$route" | jq -r 'if has("escalated") then .escalated else "missing" end')
  attempts_len=$(echo "$route" | jq '.attempts | length' 2>/dev/null || echo "0")
  last_accepted=$(echo "$route" | jq -r '.attempts[-1].accepted' 2>/dev/null)

  if [ -n "$used" ]; then
    pass_assert "$label route.usedProvider=$used"
  else
    fail_assert "$label route.usedProvider is empty — no provider recorded as serving the request: $route"
  fi

  if [ "$attempts_len" -ge 1 ] && [ "$last_accepted" = "true" ]; then
    pass_assert "$label route.attempts ends on an accepted provider ($attempts_len attempt(s))"
  else
    fail_assert "$label route.attempts=$attempts_len with last accepted='$last_accepted', want a trail ending on an accepted provider: $route"
  fi

  case "$escalated" in
    false)
      if [ "$attempts_len" -eq 1 ]; then
        pass_assert "$label not escalated: one attempt"
      else
        fail_assert "$label escalated=false but records $attempts_len attempts: $route"
      fi
      ;;
    true)
      if [ "$attempts_len" -ge 2 ]; then
        pass_assert "$label escalated after $attempts_len attempts"
      else
        fail_assert "$label escalated=true but records only $attempts_len attempt: $route"
      fi
      ;;
    *)
      fail_assert "$label route.escalated is '$escalated', want true or false"
      ;;
  esac
}

newest_activity_event() {
  local path="$1"
  pt_get "/api/activity?limit=25" >/dev/null 2>&1
  echo "$RESULT" | jq -c --arg path "$path" '[.events[] | select(.path == $path)] | last // empty'
}

activity_event_landed() {
  local path="$1" before="$2"
  ACTIVITY_EVENT=$(newest_activity_event "$path")
  [ -n "$ACTIVITY_EVENT" ] || return 1
  [ "$(echo "$ACTIVITY_EVENT" | jq -r '.timestamp')" != "$before" ]
}

# ─────────────────────────────────────────────────────────────────
start_test "activity metadata: bridge records route metadata on the event"

if [ -z "${E2E_BRIDGE_URL:-}" ]; then
  skip_test "E2E_BRIDGE_URL is not set; route metadata lands on the activity event only in bridge mode"
else
  BRIDGE_TOKEN="${E2E_BRIDGE_TOKEN:-}"
  BEFORE_TS=$(pt_on "$E2E_BRIDGE_URL" "$BRIDGE_TOKEN" newest_activity_event /navigate | jq -r '.timestamp // empty')

  pt_on "$E2E_BRIDGE_URL" "$BRIDGE_TOKEN" pt_post "/navigate?browser=${EXPECTED_BROWSER}" -d "{\"url\":\"${FIXTURES_URL}/index.html\"}"
  assert_ok "bridge navigate with browser=${EXPECTED_BROWSER}"

  if pt_on "$E2E_BRIDGE_URL" "$BRIDGE_TOKEN" wait_until "activity_event_landed /navigate '$BEFORE_TS'" "$ACTIVITY_TIMEOUT"; then
    ROUTE=$(echo "$ACTIVITY_EVENT" | jq -c '.route // empty')
    if [ -n "$ROUTE" ]; then
      assert_route_metadata "$ROUTE" "bridge activity event"
    else
      fail_assert "bridge activity event has no route metadata: $ACTIVITY_EVENT"
    fi
  fi
fi

end_test

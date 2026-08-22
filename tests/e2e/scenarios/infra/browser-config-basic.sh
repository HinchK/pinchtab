#!/bin/bash
# browser-config-basic.sh — Config/dashboard API browser field round trip.
# Covers: GET /api/config browser fields, PUT /api/config browser update,
#         proxy secret redaction.

GROUP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${GROUP_DIR}/../../helpers/api.sh"

# A config write either lands or it does not. Reporting a rejected PUT as a
# pass is how a broken dashboard write stayed green: 2xx or the test fails.
assert_config_write_ok() {
  if [ "$HTTP_STATUS" = "200" ] || [ "$HTTP_STATUS" = "204" ]; then
    pass_assert "$1"
  else
    fail_assert "$1 — PUT /api/config returned $HTTP_STATUS: $RESULT"
  fi
}

# ─────────────────────────────────────────────────────────────────
start_test "config API: GET /api/config returns browser section"

pt_get /api/config
assert_ok "get config"
assert_json_exists "$RESULT" '.config.browser' "config has browser section"

# provider is omitted from the config file unless an operator sets it, so its
# absence is a real state of this server rather than a failure. What must hold
# either way is that the key, when present, names a provider rather than null.
PROVIDER=$(echo "$RESULT" | jq -r '.config.browser.provider // empty' 2>/dev/null)
if [ -n "$PROVIDER" ]; then
  pass_assert "config browser.provider=$PROVIDER"
else
  skip_assert "browser.provider is unset on this server, so there is no value to check"
fi

end_test

# ─────────────────────────────────────────────────────────────────
start_test "config API: proxy secrets are redacted in GET response"

PROXY_SECTION=$(echo "$RESULT" | jq '.config.proxy // empty' 2>/dev/null)
if [ -n "$PROXY_SECTION" ] && [ "$PROXY_SECTION" != "null" ] && [ "$PROXY_SECTION" != "" ]; then
  PROXY_PASS=$(echo "$PROXY_SECTION" | jq -r '.password // empty' 2>/dev/null)
  if [ -z "$PROXY_PASS" ] || [ "$PROXY_PASS" = "null" ] || [ "$PROXY_PASS" = "" ] || [ "$PROXY_PASS" = "***" ]; then
    pass_assert "proxy password is redacted or absent"
  else
    fail_assert "proxy password is exposed in config GET response: $PROXY_PASS"
  fi
else
  skip_assert "no proxy configured on this server, so redaction has nothing to redact"
fi

# Check per-target proxy passwords too
TARGET_PROXIES=$(echo "$RESULT" | jq '[.config | .. | objects | select(has("proxy")) | .proxy | select(type == "object") | .password // empty] | unique' 2>/dev/null || echo "[]")
EXPOSED_COUNT=$(echo "$TARGET_PROXIES" | jq '[.[] | select(. != null and . != "" and . != "***")] | length' 2>/dev/null || echo "0")
if [ "$EXPOSED_COUNT" -eq 0 ]; then
  pass_assert "no exposed proxy passwords in config"
else
  fail_assert "found $EXPOSED_COUNT exposed proxy password(s) in config"
fi

end_test

# ─────────────────────────────────────────────────────────────────
start_test "config API: browser fields round-trip through PUT"

# Read current config.
pt_get /api/config
assert_ok "get current config"
ORIGINAL_CONFIG="$RESULT"

# Extract the browser section and make a minor update.
ORIGINAL_INNER=$(echo "$ORIGINAL_CONFIG" | jq '.config' 2>/dev/null)
UPDATED_INNER=$(echo "$ORIGINAL_INNER" | jq -c '.browser.extraFlags = "--e2e-config-roundtrip"' 2>/dev/null)

if [ -z "$UPDATED_INNER" ] || [ "$UPDATED_INNER" = "null" ]; then
  fail_assert "GET /api/config returned no .config object to update: $ORIGINAL_CONFIG"
else
  # The endpoint is a PUT and it takes a bare FileConfig, not the envelope.
  pinchtab PUT /api/config -d "$UPDATED_INNER"
  assert_config_write_ok "PUT config accepted"

  pt_get /api/config
  assert_ok "get config after PUT"
  assert_json_eq "$RESULT" '.config.browser.extraFlags' "--e2e-config-roundtrip" "browser.extraFlags survived the round trip"

  pinchtab PUT /api/config -d "$ORIGINAL_INNER"
  assert_config_write_ok "config restored"

  pt_get /api/config
  assert_ok "get config after restore"
  assert_json_eq "$RESULT" '.config.browser.extraFlags' "$(echo "$ORIGINAL_INNER" | jq -r '.browser.extraFlags // ""')" \
    "browser.extraFlags is back to its original value"
fi

end_test

# ─────────────────────────────────────────────────────────────────
start_test "config API: security.allowedDomains visible in config"

pt_get /api/config
assert_ok "get config"

assert_json_jq "$RESULT" '.config.security.allowedDomains | type == "array" and length >= 1' \
  "security.allowedDomains is a non-empty array in the config the API serves" \
  "security.allowedDomains is missing or empty, so the domain policy this server runs under is invisible to the dashboard"

end_test

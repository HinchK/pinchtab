#!/bin/bash

GROUP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${GROUP_DIR}/../../helpers/api.sh"

start_test "wait url returns immediately when the current URL already matches (#634)"

pt_post /navigate -d "{\"url\":\"${FIXTURES_URL}/form.html\"}"
assert_ok "navigate to form fixture"

pt_post /evaluate -d "{\"expression\":\"window.location.href\"}"
assert_ok "read current url"
CURRENT_URL=$(echo "$RESULT" | jq -r '.result')

WAIT_BODY=$(jq -nc --arg u "$CURRENT_URL" '{url:$u,timeout:3000}')
pt_post /wait -d "$WAIT_BODY"
assert_ok "exact url wait request"
assert_result_eq '.waited' 'true' 'exact URL wait matches the current URL'

pt_post /wait -d '{"url":"*form.html","timeout":3000}'
assert_ok "glob url wait request"
assert_result_eq '.waited' 'true' 'glob URL wait matches the current URL'

end_test

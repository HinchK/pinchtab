#!/bin/bash

GROUP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${GROUP_DIR}/../../helpers/api.sh"

start_test "keyboard-type humanize types into the focused element (#633)"

pt_post /navigate -d "{\"url\":\"${FIXTURES_URL}/form.html\"}"
assert_ok "navigate to form fixture"

pt_post /action -d '{"kind":"click","selector":"#username"}'
assert_ok "focus the username input"

pt_post /action -d '{"kind":"keyboard-type","text":"hello633","humanize":true}'
assert_ok "humanized keyboard-type into the focused element (no selector)"

pt_post /evaluate -d "{\"expression\":\"document.querySelector('#username').value\"}"
assert_ok "read the focused input value"
assert_result_eq '.result' 'hello633' 'humanized keyboard-type value persisted in the focused input'

end_test

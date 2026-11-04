#!/usr/bin/env bash
# Regression test for .github/scripts/pm-dispatch-launch.sh (#1435).
#
# The launch sequence used to be inlined identically in agents.yml's
# three dispatch steps; it now lives once in pm-dispatch-launch.sh.
# This suite drives the REAL script with a stub curl (the only network
# tool it calls) and the real jq, and pins the observable contract the
# workflow depends on:
#   happy path   -> payload {repo, title "Scheduled <Task> — <repo>",
#                   prompt, wait: true} POSTed with the minted token;
#                   GITHUB_OUTPUT gets task_id / session_id /
#                   issue_number / issue_url (the Summarize step's keys)
#   non-2xx      -> exit 1, error names the HTTP code
#   prompt fetch -> exit 1, error names the task and URL
#   empty prompt -> exit 1, error says the prompt is empty
#   no OIDC env  -> exit 1, error names the missing runner variables
#   empty token  -> exit 1, error says the mint failed
#
# Run: bash .github/scripts/pm-dispatch-launch.test.sh (exit 0 = passed)
set -u

cd "$(dirname "$0")" || exit 1
SCRIPT="./pm-dispatch-launch.sh"
[ -f "$SCRIPT" ] || { echo "FAIL: $SCRIPT not found" >&2; exit 1; }
failures=0

check() { # $1 name, $2 condition-result (0 = pass)
  if [ "$2" -eq 0 ]; then
    echo "PASS: $1"
  else
    echo "FAIL: $1"
    failures=$((failures + 1))
  fi
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"

# Stub curl: answers the script's three call shapes by URL, records the
# POST payload, and is steered per-case by STUB_* env. The shebang names
# this bash by path: the nix build sandbox has no /usr/bin/env (#978).
{ printf '#!%s\n' "$(command -v bash)"; cat <<'STUB'
url=""
payload=""
prev=""
for a in "$@"; do
  if [ "$prev" = "-d" ]; then payload=$a; fi
  case "$a" in http*) url=$a ;; esac
  prev=$a
done
case "$url" in
  *prompts/*)
    [ "${STUB_PROMPT_FAIL:-0}" = "1" ] && exit 22
    printf '%s' "${STUB_PROMPT-stub prompt body}"
    ;;
  *audience=pm-dispatch*)
    if [ "${STUB_TOKEN_EMPTY:-0}" = "1" ]; then
      printf '{"value":""}'
    else
      printf '{"value":"stub-oidc-token"}'
    fi
    ;;
  *dispatch.security.cab*)
    printf '%s' "$payload" > "$STUB_PAYLOAD_FILE"
    printf '%s\n%s' "${STUB_RESPONSE_BODY:-{\}}" "${STUB_HTTP_CODE:-201}"
    ;;
  *)
    echo "stub curl: unexpected URL: $url" >&2
    exit 99
    ;;
esac
STUB
} > "$work/bin/curl"
chmod +x "$work/bin/curl"

run_launch() { # extra env assignments as args; sets rc, out, err
  : > "$work/github-output"
  env -i PATH="$work/bin:$PATH" HOME="$work" \
    INPUT_REPO="dezren39/darwin-nix" INPUT_TASK="bolt" \
    INPUT_PROMPT_URL="https://example.test/prompts/bolt.md" \
    ACTIONS_ID_TOKEN_REQUEST_URL="https://example.test/oidc?tok=req" \
    ACTIONS_ID_TOKEN_REQUEST_TOKEN="req-token" \
    GITHUB_OUTPUT="$work/github-output" \
    STUB_PAYLOAD_FILE="$work/payload.json" \
    STUB_RESPONSE_BODY='{"task_id":"T-1","session_id":"S-1","issue_number":42}' \
    "$@" bash "$SCRIPT" > "$work/out" 2> "$work/err"
  echo $?
}

# --- happy path -------------------------------------------------------------
rc=$(run_launch)
[ "$rc" -eq 0 ]; check "happy path exits 0" "$?"
grep -qx 'task_id=T-1' "$work/github-output"; check "output task_id" "$?"
grep -qx 'session_id=S-1' "$work/github-output"; check "output session_id" "$?"
grep -qx 'issue_number=42' "$work/github-output"; check "output issue_number" "$?"
grep -qx 'issue_url=https://github.com/dezren39/darwin-nix/issues/42' "$work/github-output"
check "output issue_url derived from repo + number" "$?"
[ "$(jq -r .repo "$work/payload.json")" = "dezren39/darwin-nix" ]; check "payload repo" "$?"
[ "$(jq -r .title "$work/payload.json")" = "Scheduled Bolt — dezren39/darwin-nix" ]
check "payload title is Scheduled <Capitalized task> — <repo>" "$?"
[ "$(jq -r .prompt "$work/payload.json")" = "stub prompt body" ]; check "payload prompt is the fetched prompt" "$?"
[ "$(jq -r .wait "$work/payload.json")" = "true" ]; check "payload wait is true" "$?"

# --- non-2xx response ---------------------------------------------------------
rc=$(run_launch STUB_HTTP_CODE=500 STUB_RESPONSE_BODY='{"error":"boom"}')
[ "$rc" -eq 1 ]; check "non-2xx exits 1" "$?"
grep -q 'pm-dispatch POST /v1/sessions failed (HTTP 500)' "$work/err"
check "non-2xx error names the HTTP code" "$?"

# --- prompt fetch failure -----------------------------------------------------
rc=$(run_launch STUB_PROMPT_FAIL=1)
[ "$rc" -eq 1 ]; check "prompt fetch failure exits 1" "$?"
grep -q "failed to fetch prompt for task 'bolt'" "$work/err"
check "prompt fetch error names the task" "$?"

# --- empty prompt ---------------------------------------------------------------
rc=$(run_launch STUB_PROMPT='')
[ "$rc" -eq 1 ]; check "empty prompt exits 1" "$?"
grep -q "prompt for task 'bolt' is empty" "$work/err"
check "empty prompt error says so" "$?"

# --- missing OIDC env -----------------------------------------------------------
: > "$work/github-output"
env -i PATH="$work/bin:$PATH" HOME="$work" \
  INPUT_REPO="dezren39/darwin-nix" INPUT_TASK="bolt" \
  INPUT_PROMPT_URL="https://example.test/prompts/bolt.md" \
  GITHUB_OUTPUT="$work/github-output" \
  bash "$SCRIPT" > "$work/out" 2> "$work/err"
rc=$?
[ "$rc" -eq 1 ]; check "missing OIDC env exits 1" "$?"
grep -q 'ACTIONS_ID_TOKEN_REQUEST_URL / ACTIONS_ID_TOKEN_REQUEST_TOKEN are not set' "$work/err"
check "missing OIDC env error names the variables" "$?"

# --- empty minted token -----------------------------------------------------------
rc=$(run_launch STUB_TOKEN_EMPTY=1)
[ "$rc" -eq 1 ]; check "empty minted token exits 1" "$?"
grep -q 'failed to mint a GitHub Actions OIDC token' "$work/err"
check "empty token error says the mint failed" "$?"

if [ "$failures" -gt 0 ]; then
  echo "FAILURES: $failures"
  exit 1
fi
echo "ALL PASS"

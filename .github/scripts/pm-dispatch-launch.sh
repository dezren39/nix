#!/usr/bin/env bash
# pm-dispatch-launch.sh -- launch one scheduled agent task via pm-dispatch.
#
# Extracted from .github/workflows/agents.yml (#1435): the bolt, palette
# and suggestions dispatch steps each inlined this identical sequence
# (prompt fetch, OIDC mint, payload construction, POST /v1/sessions,
# response check, output parsing), so a fix had to be made three times
# and the copies could drift. The steps now differ only in their env
# (INPUT_REPO / INPUT_TASK / INPUT_PROMPT_URL) and call this script.
#
# Interface (env, identical to the former inline steps):
#   INPUT_REPO, INPUT_TASK, INPUT_PROMPT_URL
#   ACTIONS_ID_TOKEN_REQUEST_URL, ACTIONS_ID_TOKEN_REQUEST_TOKEN
#     (provided by the runner when the job grants id-token: write)
#   GITHUB_OUTPUT -- receives task_id, session_id, issue_number,
#     issue_url, the keys the Summarize step consumes.
# Error messages and exit codes are unchanged from the inline version.
set -euo pipefail
# Inlined pm-dispatch-start (#1367): launch a tracked
# OpenCode session via pm-dispatch (POST /v1/sessions).
if ! resolved_prompt="$(curl -fsS "$INPUT_PROMPT_URL")"; then
  echo "::error::agents dispatch: failed to fetch prompt for task '$INPUT_TASK': $INPUT_PROMPT_URL" >&2
  exit 1
fi
if [ -z "$resolved_prompt" ]; then
  echo "::error::agents dispatch: prompt for task '$INPUT_TASK' is empty: $INPUT_PROMPT_URL" >&2
  exit 1
fi
if [ -z "${ACTIONS_ID_TOKEN_REQUEST_URL:-}" ] || [ -z "${ACTIONS_ID_TOKEN_REQUEST_TOKEN:-}" ]; then
  echo "::error::agents dispatch: ACTIONS_ID_TOKEN_REQUEST_URL / ACTIONS_ID_TOKEN_REQUEST_TOKEN are not set. The calling job must grant 'permissions: id-token: write' so an OIDC token can be minted." >&2
  exit 1
fi
task_capitalized="${INPUT_TASK^}"
title="Scheduled ${task_capitalized} — ${INPUT_REPO}"
oidc_token="$(curl -sS -H "Authorization: Bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" \
  "${ACTIONS_ID_TOKEN_REQUEST_URL}&audience=pm-dispatch" | jq -r '.value // empty')"
if [ -z "$oidc_token" ]; then
  echo "::error::agents dispatch: failed to mint a GitHub Actions OIDC token (audience pm-dispatch)." >&2
  exit 1
fi
payload="$(jq -n \
  --arg repo "$INPUT_REPO" \
  --arg title "$title" \
  --arg prompt "$resolved_prompt" \
  --argjson wait true \
  '{repo: $repo, title: $title, prompt: $prompt, wait: $wait}')"
response="$(curl -sS -w $'\n%{http_code}' -X POST "https://dispatch.security.cab/v1/sessions" \
  -H "Authorization: Bearer $oidc_token" \
  -H "Content-Type: application/json" \
  -d "$payload")"
http_code="${response##*$'\n'}"
body="${response%$'\n'*}"
if [ "$http_code" -lt 200 ] || [ "$http_code" -ge 300 ]; then
  echo "::error::agents dispatch: pm-dispatch POST /v1/sessions failed (HTTP $http_code):" >&2
  echo "$body" >&2
  exit 1
fi
echo "$body"
task_id="$(printf '%s' "$body" | jq -r '.task_id // empty')"
session_id="$(printf '%s' "$body" | jq -r '.session_id // empty')"
issue_number="$(printf '%s' "$body" | jq -r '.issue_number // empty')"
issue_url=""
if [ -n "$issue_number" ]; then
  issue_url="https://github.com/${INPUT_REPO}/issues/${issue_number}"
fi
{
  echo "task_id=$task_id"
  echo "session_id=$session_id"
  echo "issue_number=$issue_number"
  echo "issue_url=$issue_url"
} >> "$GITHUB_OUTPUT"

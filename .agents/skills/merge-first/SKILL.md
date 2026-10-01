---
name: merge-first
description: "Merge-first mode: open a PR to collect Copilot findings, merge it immediately without waiting for checks or review, and move straight on to the next goal. Findings are collected in a tracking issue and only acted on if CI breaks or a finding bears on the current work. ONLY when the user explicitly asks for this mode. Never the default. If the repository requires approving reviews, get the user's explicit approval to override before every admin merge."
---

# Merge-first mode

A deliberate trade: throughput over pre-merge review. Every PR is opened, sent to Copilot, and merged
at once with admin rights. Review findings arrive *after* the merge and are triaged in one place.

**Only when the user asks for it**, in words like "merge immediately", "don't wait for checks", or
"merge-first". It is never a default, never inferred from urgency, and it ends when the session
ends.

## Before the first merge

1. **Read the branch rules.** `gh api repos/{owner}/{repo}/rulesets` and each ruleset's `rules`.
2. **If a `pull_request` rule has `required_approving_review_count > 0`, stop and ask the user**
   to approve overriding it — naming the repository and the count. An admin merge bypasses
   required approvals; that is the user's decision to make, not yours to infer from "merge
   immediately". Ask again for any other repository.
3. **Note required status checks.** Merging before they finish needs `--admin`.
4. **Create the tracking issue** — "Pending review feedback (merged PRs)" — with one heading per PR.
   Reuse it for the whole session.
5. **Use merge commits, not squash.** Squashing a PR that another branch is stacked on makes that
   branch re-merge the same work as different commits.

## The loop

1. Make a good, self-contained change on a branch cut from **current** `origin/main`.
2. Open the PR, labels passed to `gh pr create --label` (adding labels afterwards can cancel CI).
3. Request Copilot: `gh api --method POST repos/{o}/{r}/pulls/{n}/requested_reviewers -f "reviewers[]=copilot-pull-request-reviewer[bot]"`.
4. **Merge at once:** `gh pr merge {n} --merge --admin`. Pass the method: non-interactively a missing
   one is an error, and an error swallowed by a pipe looks exactly like waiting.
5. **Confirm it merged** — `gh pr view {n} --json state,mergeCommit` — then record the PR number.
6. **Immediately cut the next branch from the new `origin/main`.** `git fetch` first.
7. Return to the next goal.

## Collecting findings

- When Copilot's review lands, copy each finding into the tracking issue under that PR's heading,
  with `file:line` and the reviewer's text.
- **Do not stop to work them.** Continue the current goal unless **CI on `main` fails** or a finding
  **directly affects the thing you are building right now**.
- If you do act on one, comment on the tracking issue with what you did and the PR that did it,
  then go back to the goal.

## Watching main

- Check `main`'s CI periodically: `gh run list --branch main --limit 5`.
- **A red `main` outranks the current goal.** Fix it first, through the same loop.
- Remember CI runs in UTC; a date-sensitive test can pass locally and fail there.

## What this mode does not change

- Tests still have to exist and pass locally before you open the PR. Merging early is not
  permission to merge untested work.
- Nothing destructive: no force-push, no history rewrites.
- It does not override a user's later instruction to stop or slow down.

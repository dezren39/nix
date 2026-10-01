---
name: merge-first
description: "Merge-first mode: open a PR, request a Copilot review, merge it immediately without waiting for checks or review, and move straight on to the next goal. Findings are collected in a tracking issue and handed off at the end. ONLY when the user explicitly asks for this mode. Never the default. If the repository requires approving reviews, get the user's explicit approval to override before every admin merge."
---

# Merge-first mode

A deliberate trade: throughput over pre-merge review. Every PR is opened, sent to Copilot, and merged
at once with admin rights, before CI runs. Review findings arrive *after* the merge and are triaged
in one place. That is the point: without it, every PR circles through local tests, Copilot rounds,
CI and any merge queue before the next one can start.

**Only when the user asks for it**, in words like "merge immediately", "don't wait for checks", or
"merge-first". It is never a default, never inferred from urgency, and it ends when the session
ends. The user must be an admin and must have asked; given both, bypassing checks, reviews and any
merge queue is intended, not a risk to flag.

## Before the first merge

1. **Read the branch rules.** `gh api repos/{owner}/{repo}/rulesets` and each ruleset's `rules`.
2. **If a `pull_request` rule has `required_approving_review_count > 0`, stop and ask the user**
   to approve overriding it — naming the repository and the count. An admin merge bypasses
   required approvals; that is the user's decision to make, not yours to infer from "merge
   immediately". Ask again for any other repository.
3. **Note required status checks and whether a merge queue is enabled.** Do not assume either way.
   Merging before checks finish, or outside a queue, needs `--admin`.
4. **Create the tracking issue** — "Pending review feedback (merged PRs)". Reuse it for the whole
   session.
5. **Use merge commits, not squash.** Squashing a PR that another branch is stacked on makes that
   branch re-merge the same work as different commits.

`main` auto-deploys. That is known and accepted; you do not need to check it.

## The loop

1. Make a good, self-contained change on a branch cut from **current** `origin/main`.
2. **Test narrowly.** Run only the tests you wrote or changed, plus any you specifically believe
   the change affects. No gates, no "every test in this category or file". CI catches the rest
   after merge.
3. Open the PR, labels passed to `gh pr create --label` (adding labels afterwards can cancel CI).
4. **Request Copilot right after pushing**, every time — no waiting, no assuming it will pick the
   PR up on its own:
   `gh api --method POST repos/{o}/{r}/pulls/{n}/requested_reviewers -f "reviewers[]=copilot-pull-request-reviewer[bot]"`.
5. **Merge at once:** `gh pr merge {n} --merge --admin`. Pass the method: non-interactively a missing
   one is an error, and an error swallowed by a pipe looks exactly like waiting.
6. **Confirm it merged** — `gh pr view {n} --json state,mergeCommit`.
7. **Append the PR's heading to the end of the tracking issue** with the warning
   `WARNING: no review found yet for #{n}`. Replace the warning with the findings when the review
   lands; any heading still carrying it at the end is a PR that was never reviewed.
8. **Immediately cut the next branch from the new `origin/main`.** `git fetch` first.
9. Return to the next goal.

## Collecting findings

- When Copilot's review lands, append every finding to the tracking issue under that PR's heading,
  in one standard format built from what the API returns:
  `gh api repos/{o}/{r}/pulls/{n}/comments --paginate --jq '.[] | {html_url, path, line, body}'`
  (the review summary's link is `html_url` from `gh api repos/{o}/{r}/pulls/{n}/reviews`).

  ```markdown
  - [`{path}:{line}`]({html_url})
    > {body}

    _Note (optional):_ {one or two lines}
  ```

  The link goes straight to the comment (`.../pull/{n}#discussion_r{id}`). The note is optional — a
  short summary, an opinion on whether the finding is valid, or why it happened. Keep it brief; do
  not stop to investigate.
- **Do not stop to work them.** Treat security and data findings exactly like every other finding —
  no special category. Only stop for:
  - code that breaks a working page,
  - a failing build or red CI on `main`,
  - a problem that will directly interfere with your next feature.
- If you do act on one, comment on the tracking issue with what you did and the PR that did it,
  then go back to the goal.

## Watching main

- Check `main`'s CI periodically: `gh run list --branch main --limit 5`.
- **A red `main` outranks the current goal.** Fix it first, through the same loop.
- Remember CI runs in UTC; a date-sensitive test can pass locally and fail there.

## Finishing

After the last PR is merged, **always**:

1. Write a comprehensive `handoff.txt`: every PR merged (number, title, merge commit), every
   finding still open with `file:line`, every PR still showing the no-review warning, the state of
   `main`'s CI, anything you fixed along the way, and what you would do next.
2. Put it on the tracking issue: `gh issue comment {issue} --body-file handoff.txt`.
3. Then, depending on the situation, either begin working through the findings — usually the
   intended last step, through the same loop — or move on to your next task. The handoff is
   written either way.

## What this mode does not change

- Tests you wrote must exist and pass locally before you open the PR. Merging early is not
  permission to merge untested work.
- Nothing destructive: no force-push, no history rewrites.
- It does not override a user's later instruction to stop or slow down.

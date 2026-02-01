# Fix

Analyze a failed GitHub Actions job or other job/command/etc, diagnose the issue, fix it, and retry.

## Arguments

$ARGUMENTS - Optional description of which job to fix and any modifications to the plan.

Examples:
- `the windows oseries ami build`
- `run 21540547567`
- `job 62074290289`
- `the last packer build, focus on the powershell script errors`

If no arguments provided, check recent conversation context for job references.

## Instructions

### Step 1: Identify the Job

First, determine which job to analyze:

1. **If $ARGUMENTS specifies a run/job ID**: Use that directly
2. **If $ARGUMENTS describes the job**: Search recent runs to find it
3. **If no arguments**: Look for job references in recent conversation context
4. **If unclear or sensitive**: Ask the user
If user asks for different custom input then confirm back until they say yes to what you ask without edit.

Check the recent runs from this branch first, if there wasn't a recent failure confirm with user whether they want to look at other branch's executions
```bash
# List recent workflow runs (with or without branch/ref)
gh run list --limit=10

# Get specific run details
gh run view <RUN_ID> --json status,conclusion,jobs
```

### Step 2: Validate Before Proceeding

If any of these conditions are true, **DO NOT proceed automatically**:

- The job involves production deployments
- The job name or inputs contain words like "publish", "release", "production", or "main"
- The job modifies infrastructure (terraform, cloudformation)
- No clear job was identified from context or $ARGUMENTS
- The job hasn't actually failed (status != failure)
- The job found wasn't run recently or within this session

Instead, ask the user something like:

```
I found [job description]. This appears to be [sensitive/unclear].

Before proceeding, I need to confirm:
1. Is this the correct job? [Yes/No/Describe different job]
2. Should I analyze and attempt to fix it? [Yes/No/Describe different fix strategy]
3. Do you want me to rerun the job? [Rerun/Describe different rerun strategy]
```
If user asks for different custom input then confirm back until they say yes to what you ask without edit.

If the job is a remote job and this is the first fix attempt in this session:
- Confirm whether you should commit and push the changes
  - State the name of the current branch to push to
  - Offer an alternative branch, creating a new targeted branchname if from a protected branch, creating a new related branch name if its from a feature branch
  - Offer to make a new branch from main that tries to re-implement the current changes independently, using commits, pr and diffs to see what changes exist. Ensure to note the original branch in your new branches commits or draft pr so future sessions can also review the history.
- If there are uncommitted/unpushed changes, confirm whether to commit/stash/make a new branch/etc.
- If the final selected branch is an important branch such as `main` or `releases/` or anything that doesn't seem like a feature branch then re-ask after receiving the answer to the first question.
  - Do not commit to anything looking like a shared/protected/important branch without confirming.
If user asks for different custom input then confirm back until they say yes to what you ask without edit.

### Step 3: Analyze the Failure

Once confirmed, fetch and analyze the logs:

An example for a github action job:
```bash
# Get job logs (last 500 lines usually sufficient)
gh api repos/{owner}/{repo}/actions/jobs/{JOB_ID}/logs 2>&1 | tail -500

# For more context, search for specific patterns
gh api repos/{owner}/{repo}/actions/jobs/{JOB_ID}/logs 2>&1 | grep -B5 -A10 "error\|failed\|Error\|FAILED"
```

Depending on context you may need to run local commands or query other services.

Look for:
- Error messages and stack traces
- Failed commands with exit codes
- Missing files or directories
- Permission issues
- Timeout errors
- Resource/dependency issues

If you can't find a clear issue, ask the user and provide your best guesses.
If user asks for different custom input then confirm back until they say yes to what you ask without edit.

### Step 4: Propose the Fix

Present your analysis to the user:

```
## Job Analysis

**Job**: [name] (ID: [id])
**Status**: Failed after [duration]
**Error**: [primary error message]

## Root Cause

[Explanation of what went wrong]

## Proposed Fix

I plan to make these changes:

1. [File: path/to/file.ext]
   - [Description of change]
   ```diff
   - old code
   + new code
   ```

2. [Additional changes...]

## After Fix

I will:
- Commit with message: "[commit message]"
- Push to branch: [branch]
- Trigger workflow: [workflow name]
- Monitor for up to [30 default, if a user specifically suggested a different timeout, use that instead] minutes, checking every [30 default if a user specifically suggested a different timeout, use that instead] seconds

Proceed with this plan? [Yes/No/Edit]
If user asks for different custom input then confirm back until they say yes to what you ask without edit.

```

### Step 5: Confirm Before Changes

Wait for user confirmation. If they request edits, update the plan and confirm again.

Only proceed when you have explicit confirmation without edits.

Ask user if you want them to open a --web view

### Step 6: Apply Fix and Retry

Github Actions Example:

```bash
# Make the code changes
# ... (use edit tools)

# Commit and push
git add [files]
git commit -m "[message]"
git push

# Trigger the workflow
gh workflow run [workflow.yml] --ref [branch] [parameters]

# Get the new run ID
sleep 5
NEW_RUN_ID=$(gh run list --workflow=[workflow.yml] --branch=[branch] --limit=1 --json databaseId -q '.[0].databaseId')
```

### Step 7: Monitor the New Run

Monitor with a 30-minute timeout (if a user specifically suggested a different timeout, use that instead):
Sleep between checks for 30-seconds (if a user specifically suggested a different wait, use that instead):
```bash
RUN_ID=$NEW_RUN_ID
START_TIME=$(date +%s)
MAX_DURATION=$((30 * 60))
LAST_STATUS=""

echo "Monitoring run $RUN_ID (max 30 minutes)..."

while true; do
  RESULT=$(gh run view $RUN_ID --json status,conclusion,jobs 2>&1)
  STATUS=$(echo "$RESULT" | jq -r '.status')
  CONCLUSION=$(echo "$RESULT" | jq -r '.conclusion')

  CURRENT_TIME=$(date +%s)
  ELAPSED=$((CURRENT_TIME - START_TIME))
  ELAPSED_MIN=$((ELAPSED / 60))

  NEW_STATUS="$STATUS ($CONCLUSION)"
  if [ "$NEW_STATUS" != "$LAST_STATUS" ]; then
    echo "[${ELAPSED_MIN}m] $NEW_STATUS"
    LAST_STATUS="$NEW_STATUS"
  fi

  if [ "$STATUS" = "completed" ]; then
    echo "Run completed: $CONCLUSION"
    break
  fi

  if [ $ELAPSED -ge $MAX_DURATION ]; then
    echo "Timeout reached. Last status: $STATUS ($CONCLUSION)"
    break
  fi

  sleep 30
done
```

### Step 8: Report Results

Review the logs and details of the run.

Provide a summary:

**If successful:**
```
## Success

The job completed successfully after [duration].

**Run**: [URL]
**Changes made**: [summary of fixes]
**Key Evidence**: [logs output code etc. that are core to the fix]
```
If the current branch doesn't have a pr
- ask if you want one made
- if you branched off a feature offer to make a pr into the other feature
- offer to make a pr into main
- offer user chance for other strategy and then re-ask until they confirm
If you are making a pr,
- ask if it should be open or draft:
  - open
  - draft
  - allow user provided instruction and then re-ask until they confirm
- list the proposed merge of <brancha> -> <branchb> and confirm it looks good before editing, allow user to provide instruction and then re-ask until they confirm
- desribe a rough outline and highlights of any pr body, allow user to provide instruction and then re-ask until they confirm
- list the proposed pr title and confirm it looks good before editing, allow user to provide instruction and then re-ask until they confirm
- describe a rough outline and highlights of any pr body modification, allow user to provide instruction and then re-ask until they confirm

If a pr already exists,
- review the pr title/body
- ask user if you should update the pr title (yes/no)
- ask user if you should update the pr body (yes/no)
- list the pr title change and confirm it looks good before editing, allow user to provide instruction and then re-ask until they confirm
- describe a rough outline and highlights of any pr body modification, allow user to provide instruction and then re-ask until they confirm

Before pushing or modifying any pr, provide a short summary, get a final confirmation, then execute.
If user asks for different custom input then confirm back until they say yes to what you ask without edit.

**If failed again:**
```
## Still Failing

The job failed again after [duration].

**Error**: [new error message]
**Previous fix**: [what we changed]

The issue may be:
- [possible cause 1]
- [possible cause 2]
- [etc...]

Would you like me to:
1. Analyze the new failure and try again
2. Show full logs for manual review
3. Stop here
4. user provided instruction
```
If user asks for different custom input then confirm back until they say yes to what you ask without edit.

Ask user if you want them to open a --web view

**If timeout:**
Github Actions example, modify as-needed depending on context:

```
## Timeout

The job is still running after 30 minutes.

**Current status**: [status]
**Run URL**: [URL]

You can continue monitoring with:
gh run watch [RUN_ID]
```

Ask user if you want them to open a --web view


# Context Management Protocol

## Core Concept

Use `/tmp/opencode/` markdown files to track state, logs, and progress. Files are append-only by default.

## File Structure

```
/tmp/opencode/
├── session.<sid>.md                              # Session log (append-only)
├── session.<sid>.goals.md                        # Top-level goals list (modifiable + changelog)
├── session.<sid>.goals.<goalname>.md             # Named goal details (modifiable + changelog)
├── session.<sid>.goals.<N>.<goalname>.md         # Ordered goal details (modifiable + changelog)
├── request.<rid>.md                              # Request log (append-only)
├── request.<rid>.todo.md                         # Top-level todos list (modifiable + changelog)
├── request.<rid>.todo.<N>.md                     # Todo item details (modifiable + changelog)
├── request.<rid>.todo.<N>.<todoname>.md          # Named todo item details (modifiable + changelog)
├── <anyfile>.scratch.md                          # Working notes for any file (modifiable, changelog optional)
├── <anyfile>.changelog.md                        # Changes to modifiable files (append-only)
├── request.<rid>.logs.<name>.md                  # External data captures - markdown (append-only)
└── request.<rid>.logs.<name>.<ext>               # External data captures - any format (append-only)
```

**Note**: Files can link to other files anywhere, including other folders. Use relative or absolute paths.

## Naming Conventions

- `<sid>` = 6-char session ID
- `<rid>` = 6-char request ID
- `<N>` = number (for ordering, optional)
- `<goalname>` = short hyphenated name (e.g., `setup-ssh`, `fix-build`)
- `<todoname>` = short hyphenated name (e.g., `analyze-logs`, `update-config`)
- `<name>` = descriptive name for logs/artifacts

## ID Rules

- `<sid>` = 6-char session ID (reuse until user requests new)
- `<rid>` = 6-char request ID (new per distinct user goal)
- Document IDs at file creation; if missing, create and document

## File Types

| Pattern | Write Mode | Changelog |
|---------|------------|-----------|
| `session.<sid>.md`, `request.<rid>.md` | append-only | No |
| `*.logs.<name>.*` | append-only | No |
| `*.changelog.md` | append-only | No |
| `*.goals.md`, `*.goals.<name>.md` | modifiable | **Required** |
| `*.todo.md`, `*.todo.<N>.md`, `*.todo.<N>.<name>.md` | modifiable | **Required** |
| `*.scratch.md` | modifiable | Optional (recommended) |

**Changelog Rule**: Every modifiable file except `.scratch.md` should have a `.changelog.md`. Scratch files may have one too (recommended for complex work).

## Append-Only Rules

1. Complete open markdown structures before appending
2. Each append starts on new line
3. Files end with newline
4. Use timestamps: `[YYYY-MM-DD HH:MM]`
5. Include timestamps on log lines marking significant steps (good practice for all files)

## What to Log Where

| Event | Request File | Session File |
|-------|--------------|--------------|
| User input changing scope | Yes | Yes |
| External command run | Yes | If modifies external state |
| Analysis/summary | Yes | One-line + "see request" |
| Large logs captured | Link only | No |
| Todo create/start/complete | Yes + timestamp | No |
| Goal create/start/complete | Yes + timestamp (if request open) | Yes + timestamp |
| PR/job/external modifications | Yes | Yes |

## Compaction Header (Always Include)

```markdown
## Active Context
- Session: /tmp/opencode/session.<sid>.md - [purpose]
- Request: /tmp/opencode/request.<rid>.md - [purpose]
- Current Todo: [N] - [description]
```

## Session Lifecycle

1. **Start**: Create session file, document initial request
2. **Continue**: Reuse session ID; link new requests
3. **Switch**: If new request doesn't align:
   - Summarize current session
   - Propose new session
   - List carryover items
   - Confirm with user
4. **End**: Document reason at file bottom

## Request Lifecycle

1. **Start**: Link to session at top; create todo.md file
2. **Continue**: User inputs refining same goal stay in same request
3. **Complete**: Ask user to confirm; mark complete as last line
4. **New Request**: User may request a new request without exact wording (e.g., "let's do something else", "different task", "start fresh")
   - If unsure whether user wants a new request, **ask to confirm**
   - If confirmed new request: mark old request complete with reason, create new request

## Todo Lifecycle

1. **Create**: Add todo to `todo.md` with number and description; log in request file with timestamp
2. **Start**: Mark todo in-progress in `todo.md`; create `todo.<N>.md` subfile if needed
3. **Work**: Document progress in todo subfile; update scratch files as needed
4. **Complete**:
   - Mark done in `todo.md`
   - Append `[COMPLETE]` to `todo.<N>.md`
   - Log completion in request file with timestamp
5. **Update changelog**: Log all modifications to `todo.md.changelog.md`

### Todo Recovery After Compaction

If unsure of current todo state:

1. Check context/compaction notes for current todo
2. Fallback: Read `todo.md` to find next incomplete item
3. Tail `todo.<N>.md` to check if done (`tail -1` for `[COMPLETE]`)
4. If not done: Review `todo.<N>.md` content and compare with compaction notes
5. Decide: **Continue** where left off OR **Restart** the todo step

### Continue vs Restart Decision

- **Continue**: Pick up from last documented progress point
- **Restart**: Begin the todo step from scratch

**Re-confirm with user if restart/continue will:**
- Trigger a new build, job, or CI/CD pipeline
- Re-run expensive or time-consuming operations
- Make external API calls or modifications

**Rollback Safety**: If rolling back or restarting, ensure you do NOT undo previous completed todos. Check `todo.md` and completed `todo.<N>.md` files before making changes.

Update request file and session file as appropriate when recovering or changing todo state.

## Recovery After Compaction

Do NOT cat entire files back. Instead:

1. Read session file tail (find current request)
2. Read request file tail (find current todo)
3. Check todo status: `tail -1 request.<rid>.todo.<N>.<todoname>.md`
4. Load only relevant todo subfiles
5. Resume from last incomplete todo

## File Documentation

When creating any file:
1. Log in request file: `[timestamp] Created: <path> - <purpose>`
2. Log in session file (if significant)
3. Output to conversation context

## Subfile Rules

- Any file can have a `.scratch.md` for working notes
- Every modifiable file (except scratch) should have a `.changelog.md`
- Scratch files may also have a changelog (recommended for complex work)
- Subfiles inherit the parent's base path: `parent.md` → `parent.scratch.md`
- Nested subfiles are allowed: `request.<rid>.todo.1.scratch.changelog.md`
- Files can link to files in other directories using relative or absolute paths

## Marking Completion

- Todo: Append `[COMPLETE]` as final line of subfile
- Request: Append `[COMPLETE] <timestamp> by session.<sid>` as final line
- Goal: Append `[COMPLETE] <timestamp>` to goals file
- Session: Append `[ENDED] <timestamp> - <reason>` as final line

# Safety Rules

1. **Never** force push or modify protected branches without explicit permission
2. **Never** commit secrets, credentials, or sensitive data
3. **Always** confirm before making changes to production-related workflows
4. **Always** use `--ref` to target the correct branch when triggering workflows
5. **Always** ensure you are following the context management protocol.
6. **Stop and ask** if the fix seems risky or the scope is unclear
7. **Stop and ask** if the correct inputs for the job are unclear
8. **Stop and ask** if user asks for different custom input then confirm back until they say yes to what you ask without edit.

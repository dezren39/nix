# Why

Analyze a successful GitHub Actions job or other job/command/etc to understand WHY it succeeded - what changed, how it differs from other branches/PRs, and whether the success is genuine (all steps ran properly).

## Arguments

$ARGUMENTS - Optional description of what to analyze and comparison parameters.

Examples:
- `the windows oseries ami build`
- `run 21540547567`
- `compare to SRE-2542 and SRE-2542a branches`
- `skip main, just compare to feature-x branch`
- `compare to PR #123`
- `focus on the ssh enablement changes`
- `check logs from /path/to/logs.txt`
- `compare against last 3 successful runs on main`

If no arguments provided, check recent conversation context for job references.

## Instructions

### Step 1: Identify the Job/Run

First, determine which successful job to analyze:

1. **If $ARGUMENTS specifies a run/job ID**: Use that directly
2. **If $ARGUMENTS describes the job**: Search recent runs to find it
3. **If no arguments**: Look for job references in recent conversation context
4. **If unclear**: Ask the user

Example commands you can use, modified depending on context:
```bash
# List recent workflow runs
gh run list --limit=10

# Get specific run details
gh run view <RUN_ID> --json status,conclusion,jobs

# List runs for a specific workflow
gh run list --workflow=<workflow.yml> --limit=10
```

If user provides custom input, confirm your interpretation back until they accept without modification.

### Step 2: Gather User Requirements

Before proceeding, clarify comparison parameters with the user:

```
## Analysis Configuration

**Job identified**: [name] (ID: [id])
**Status**: Successful
**Branch**: [branch name]

I need to understand what you want to compare and analyze:

1. **Compare to main/master?** [Yes/No/Skip]
2. **Compare to other branches?** [List branches, e.g., "SRE-2542, SRE-2542a"]
3. **Compare to specific PRs?** [List PR numbers]
4. **Compare to other successful runs?** [Describe which runs]
5. **What specifically to look for?** [e.g., "ssh enablement changes", "script execution differences"]
6. **Any external logs/context to review?** [File paths or URLs]

Please confirm or adjust these parameters.
```

If user provides custom input, confirm your interpretation back until they accept without modification.

### Step 3: Validate Comparison Compatibility

**CRITICAL**: Before comparing runs, verify they are actually comparable:

```bash
# Get job details for the successful run
gh run view <SUCCESS_RUN_ID> --json jobs -q '.jobs[] | {name, conclusion, steps: [.steps[] | {name, conclusion}]}'

# Get job details for comparison run(s)
gh run view <COMPARISON_RUN_ID> --json jobs -q '.jobs[] | {name, conclusion, steps: [.steps[] | {name, conclusion}]}'
```

Check for compatibility issues:

1. **Skipped steps**: If one run skipped major steps that the other ran, flag this:
   ```
   WARNING: Comparison may be invalid

   The run on [branch-a] skipped these steps:
   - [step name 1]
   - [step name 2]

   But the successful run on [current branch] executed them.

   This could make log comparison misleading. Proceed anyway? [Yes/No/Find different comparison/other]
   ```

2. **Different inputs/parameters**: Check workflow inputs match
3. **Different job configurations**: Verify matrix/environment settings align
4. **Different workflow versions**: Note if the workflow file itself changed

If comparisons are incompatible, ask user how to proceed before continuing.

If user provides custom input, confirm your interpretation back until they accept without modification.

### Step 4: Analyze the Success

Fetch and analyze logs to verify genuine success:

```bash
# Get job logs
gh api repos/{owner}/{repo}/actions/jobs/{JOB_ID}/logs 2>&1 > /tmp/success_logs.txt

# Check for less obvious errors/warnings that didn't fail the job
grep -i "warn\|error\|fail\|exception\|skip" /tmp/success_logs.txt | grep -v "0 errors\|0 warnings\|no error"

# Verify all expected steps completed
gh run view <RUN_ID> --json jobs -q '.jobs[] | .steps[] | "\(.name): \(.conclusion)"'
```

Verify:
- [ ] Job ran from start to end without early termination
- [ ] All expected steps executed (not skipped)
- [ ] No hidden errors masked by `|| true` or `continue-on-error`
- [ ] Exit codes were all 0 for critical steps
- [ ] Expected outputs/artifacts were produced

If anything looks suspicious, note it for the report.

### Step 5: Diff Analysis

#### 5a: Code Differences

```bash
# Compare current branch to main
git diff main...HEAD --stat
git diff main...HEAD

# Compare to specific branches
git diff <other-branch>...HEAD --stat
git diff <other-branch>...HEAD

# Compare specific files that might be relevant
git diff main...HEAD -- path/to/relevant/files/
```

#### 5b: Log Differences

```bash
# Get logs from comparison runs
gh api repos/{owner}/{repo}/actions/jobs/{COMPARISON_JOB_ID}/logs 2>&1 > /tmp/comparison_logs.txt

# Compare log structure (steps executed)
diff <(grep "^##\[group\]" /tmp/success_logs.txt) <(grep "^##\[group\]" /tmp/comparison_logs.txt)

# Look for significant differences excluding timestamps and expected changes
diff /tmp/success_logs.txt /tmp/comparison_logs.txt | grep -v "^\d\+[acd]\d\+" | head -100
```

When comparing logs:
- Filter out timestamp differences
- Filter out expected differences (e.g., branch names, commit SHAs)
- Focus on structural differences (different commands run, different outputs)
- Note any steps that produced different results

#### 5c: Configuration Differences

```bash
# Check workflow file changes
git diff main...HEAD -- .github/workflows/

# Check any config files that might affect the build
git diff main...HEAD -- packer/ scripts/ *.json *.yml *.yaml
```

### Step 6: Report Findings

Present a comprehensive analysis:

```
## Success Analysis Report

**Job**: [name] (ID: [id])
**Branch**: [branch]
**Run URL**: [url]
**Duration**: [duration]
**Completed**: [timestamp]

---

## Why It Works

[Clear explanation of what made this run succeed]

- [Key factor 1]
- [Key factor 2]
- [etc.]

---

## What Changed

### Changes That Enabled Success

[List the specific changes that fixed/enabled the functionality]

1. **[File: path/to/file]**
   - [Description of change and why it matters]
   ```diff
   - old code
   + new code
   ```

2. **[Additional changes...]**

### Minor/Unrelated Changes

[List changes that may not be directly related to the fix but are worth noting]

- [Change 1]: [Brief description, potential impact]
- [Change 2]: [Brief description, potential impact]

---

## Branch Comparisons

### vs. main/master
[Summary of differences from main branch]

### vs. [other-branch-1]
[Summary of differences, what this branch tried differently]

### vs. [other-branch-2]
[Summary of differences, what this branch tried differently]

---

## Log Analysis

### Execution Verification
- All steps completed: [Yes/No]
- Hidden errors found: [Yes/No - list if any]
- Warnings: [List any significant warnings]

### Log Differences from Comparison Runs

[Note significant differences in log output between successful run and comparison runs]

- [Difference 1]
- [Difference 2]

---

## Analysis Summary

[2-4 paragraph summary of findings]

[Paragraph 1: What the change accomplished and why it works]

[Paragraph 2: How it compares to other attempted solutions]

[Paragraph 3: Any concerns, caveats, or recommendations]

[Paragraph 4: Confidence level and any remaining questions]
```

If user provides custom input, confirm your interpretation back until they accept without modification.

### Step 7: Optional PR Actions

**IMPORTANT**: Do not make any changes without explicit user confirmation.

After presenting the analysis, ask:

```
## Optional Actions

Would you like me to help with any of the following?

1. **Review existing PR** (if one exists)
   - Check if PR title/body accurately describes the changes
   - Suggest improvements

2. **Create a new PR** (if none exists)
   - Draft PR with analysis summary

3. **Add analysis to PR**
   - Add a "What Was Fixed" or "Why This Works" section to PR body

4. **Export analysis**
   - Save this analysis to a file

5. **No changes needed**
   - Just keep the analysis for reference

Select an option: [1/2/3/4/5]
```

If user provides custom input, confirm your interpretation back until they accept without modification.

#### If Reviewing/Modifying PR:

```
## PR Review

**Current PR**: #[number]
**Title**: [current title]
**Body summary**: [brief summary of current body]

### Assessment

Does the PR adequately cover the changes/fixes?
- Title accuracy: [Good/Needs improvement]
- Body completeness: [Good/Needs improvement]
- Missing information: [List any gaps]

### Proposed Changes

**Update title?** [Yes/No]
- Current: "[current title]"
- Proposed: "[new title]"
- User-provided alternative

**Update body?** [Yes/No]
- all proposed changes
- Add "What Was Fixed" section: [Yes/No]
- Add/modify/etc. yes/no
- Other modifications: [describe]

Proceed with these changes? [Yes/No/Edit]
```

If user provides custom input, confirm your interpretation back until they accept without modification.

Wait for explicit confirmation before making ANY changes.

#### If Creating New PR:

```
## New PR Proposal

**Base branch**: [main/master/other]
**Head branch**: [current branch]

**Proposed title**: "[title]"

**Proposed body outline**:
- Summary section
- Changes section
- What Was Fixed section (from analysis)
- Testing notes
- Details (foldable details block with header details for any large or granular explanations)

Create this PR? [Yes/No/Edit]
- Draft or Ready? [Draft/Ready]
```

If user provides custom input, confirm your interpretation back until they accept without modification.

Wait for explicit confirmation before creating.

### Step 8: Execute Approved Actions (If Any)

Only after explicit user confirmation:

```bash
# If updating PR body
gh pr edit <PR_NUMBER> --body "$(cat <<'EOF'
[new body content]
EOF
)"

# If updating PR title
gh pr edit <PR_NUMBER> --title "[new title]"

# If creating new PR
gh pr create --title "[title]" --body "$(cat <<'EOF'
[body content]
EOF
)" --base [base-branch] [--draft]
```

After any modification, show the result:

```
## Action Complete

**What was done**: [description]
**PR URL**: [url]

Please verify the changes look correct.
```

## Handling External Logs/Context

If user/previous-context provides external logs or context files:

```bash
# Read local log file
cat /path/to/logs.txt

# Fetch from URL (if GitHub)
gh api [endpoint]
```

When analyzing external logs:
1. Identify the configuration/inputs used in that run
2. Compare to the current successful run's configuration
3. Flag any significant configuration differences before comparing outputs
4. Focus comparison on sections with matching configurations

## Re-confirmation Protocol

Whenever the user provides custom input or modifications to any step:

1. Acknowledge their input
2. Restate your interpretation of what they want
3. Ask for confirmation: "Is this correct? [Yes/No]"
4. If they say No or provide further modifications, repeat steps 1-3
5. Only proceed when they confirm "Yes" without additional modifications

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

## Safety Rules

1. **READ-ONLY by default** - This skill is for analysis, not modification
2. **Never** commit, push, or modify code without explicit permission
3. **Never** re-run jobs or trigger workflows
4. **Never** modify PRs without explicit confirmation
5. **Always** ensure you are following the context management protocol.
6. **Always** confirm comparison parameters before analysis
7. **Always** validate comparison compatibility before comparing runs
8. **Always** re-confirm if user provides custom input until they accept without modification
9. **Always** note when comparisons may be invalid due to different configurations
10. **Stop and ask** if the analysis scope is unclear
11. **Stop and ask** if comparison runs have significantly different configurations
12. **Stop and ask** before ANY action that would modify anything
13. **Stop and ask** if user asks for different custom input then confirm back until they say yes to what you ask without edit.

# Subagent-First Workflow

**Your default approach should be to use subagents.** Delegate work to subagents (Task tool) unless you have a specific reason not to. Subagents run autonomously with their own context windows, enabling parallel execution and keeping the main conversation focused. Reaching for a subagent should be your first instinct — only skip it when the task is truly trivial.

## When to use subagents

**Use subagents whenever it makes sense — which is most of the time:**

- **Codebase exploration** (`explore`): Finding files, searching for patterns, understanding code structure, answering questions about the codebase. Use before making changes to gather context.
- **Multi-step research or implementation** (`general`): Anything requiring multiple tool calls that can run independently — refactoring across files, running tests, investigating bugs, building features.
- **Parallel workstreams**: When multiple independent tasks exist, launch them all as subagents simultaneously rather than doing them sequentially.
- **Any task you're unsure about**: If you're debating whether to use a subagent, use one.

## Subagent types

These are the commonly available types — others may be available depending on the session and plugins. Check what's offered before assuming only these exist.

| Type         | Use for                                                                                                                                                                                   |
| ------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `explore`      | Fast codebase search: find files by pattern, search code for keywords, answer structural questions. Specify thoroughness: "quick", "medium", or "very thorough".                          |
| `general`      | General-purpose agent for researching complex questions and executing multi-step tasks in parallel. Can use all tools. Use for implementation, bug fixing, refactoring, running commands. |
| `build`        | Full development agent with all tools enabled. Use for substantial implementation work, file operations, and system commands.                                                             |
| `plan`         | Analysis and planning agent. Edits and bash require approval. Use for code review, architecture analysis, and creating plans without accidental changes.                                  |
| `bootstrapper` | Analyzes a request and creates exploration branches with scopes. Use when planning or decomposing a complex task into parallel exploration paths.                                         |
| `probe`        | Evaluates branch Q&A and decides whether to ask more or complete. Use for iterative refinement of answers and deep-dive investigations.                                                   |

## Guidelines

1. **Subagent first.** Your default action for any non-trivial task should be launching a subagent. Only do work inline if it's a single read, grep, or edit you already have full context for.
2. **Explore before editing.** Before modifying code, launch an `explore` subagent to understand the current state — related files, callers, dependencies, conventions.
3. **Parallelize aggressively.** If you need to search multiple things or do independent tasks, launch multiple subagents in a single message.
4. **Be specific in prompts.** Tell the subagent exactly what to do, what to return, and whether it should write code or just research. Include file paths and function names you already know.
5. **Summarize results.** When a subagent returns, relay the key findings to the user concisely — don't just silently consume the output.
6. **Chain when needed.** Use subagent results to inform the next step. An explore subagent's findings should feed into a general subagent's implementation task.

## Only skip subagents for

- Reading a single known file (use Read directly)
- Simple glob/grep on a known path (use Glob/Grep directly)
- Trivial one-shot edits where you already have full context

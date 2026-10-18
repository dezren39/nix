# Example scripts for Antigravity (AGY)

Each script is run from AGY's `run_command`, the way `SKILL.md` describes:
point `mcpx run` at a copy of the file, or copy one into
`.mcpx/scripts/` to run it by name. `mcpx run` writes the generated
client (`mcpx-client.ts`) beside the script it runs, so run a copy
rather than the file inside a checkout of mcpx.

| script | what it shows | try it |
| --- | --- | --- |
| `compact-call.ts` | calling one tool and printing only a bounded summary, so a large result does not bloat the AGY transcript | `mcpx run compact-call.ts codedb.status '{}'` |
| `chain-docs-and-code.ts` | chaining two servers in one script — library docs plus the local code index — and printing only the answer from each | `mcpx run chain-docs-and-code.ts react "useCallback dependencies"` |

Both exit 2 with a usage line when called without arguments, like the
`mcpx-basics` examples.

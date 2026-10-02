# Example scripts

Each one takes the namespace and tool on the command line, so it works
against whatever servers you have. The "try it" column uses the filesystem
server from `getting-started.md`, added in the directory you run from; it
refuses paths outside that directory. Copy one into `.mcpx/scripts/` to run it
by name, or run it by path:

```sh
mcpx run path/to/find-tool.ts screenshot
```

`mcpx run` writes the generated client (`mcpx-client.ts`) beside the script
it runs, so run a copy rather than the file inside a checkout of mcpx.

| script | what it shows | try it |
| --- | --- | --- |
| `find-tool.ts` | `search()` and `describe()` inside a script, no server started | `mcpx run find-tool.ts read file` |
| `call-any.ts` | `call(ns, tool, args)` by name, and measuring before printing | `mcpx run call-any.ts fs.list_allowed_directories` |
| `filter-in-script.ts` | reporting a result's shape and one field, not the whole result | `mcpx run filter-in-script.ts fs.get_file_info "{\"path\":\"$PWD\"}" content` |
| `handle-errors.ts` | `ToolError` (the tool said `isError`) versus any other failure | `mcpx run handle-errors.ts fs.read_text_file '{"path":"/nope"}'` (exits 1) |
| `parallel.ts` | several calls at once with `Promise.allSettled` | `mcpx run parallel.ts fs.get_file_info "{\"path\":\"$PWD\"}" '{"path":"/nope"}'` |
| `save-artifact.ts` | keeping a large or binary result as an artifact, printing only its reference | `mcpx run save-artifact.ts chrome_devtools.take_screenshot '{}' shot.png` |
| `read-resource.ts` | reading a resource a server publishes | `mcpx resources`, then `mcpx run read-resource.ts <ns> <uri>` |

All seven are run against mcpx's test server on deno, bun and node before
they ship (`TestSkillExamplesRun` in `internal/e2e`). Every one exits 2 with a
usage line when called without arguments.

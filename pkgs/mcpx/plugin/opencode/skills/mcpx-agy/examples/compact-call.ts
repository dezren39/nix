// Call one tool from AGY and print only a bounded summary.
//
//   mcpx run compact-call.ts <namespace>.<tool> '{"arg":"value"}'
//
// In Antigravity every byte a tool returns lands in transcript.jsonl, so
// the point of running through `mcpx exec` / `mcpx run` instead of a
// direct MCP call is that only what this script prints comes back.
// Measure the result first, then print a slice of it.
const [target, json = "{}"] = Deno.args;
const dot = target?.indexOf(".") ?? -1;
if (dot < 1) {
  console.error("usage: mcpx run compact-call.ts <namespace>.<tool> [json-args]");
  Deno.exit(2);
}
const r = await call(target.slice(0, dot), target.slice(dot + 1), JSON.parse(json));

const text = typeof r === "string" || r instanceof String ? String(r) : JSON.stringify(r);
console.log(`${text.length} chars`);
console.log(text.slice(0, 500));

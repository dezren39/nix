// Call any tool by name and print a bounded view of the result.
//
//   mcpx run call-any.ts <namespace>.<tool> '{"arg":"value"}'
//
// call() reaches tools added after the client was generated, too. The result
// arrives unwrapped: structured output and JSON-in-text are already parsed,
// plain text is a string. The full MCP envelope is on .raw.
const [target, json = "{}"] = Deno.args;
const dot = target?.indexOf(".") ?? -1;
if (dot < 1) {
  console.error("usage: mcpx run call-any.ts <namespace>.<tool> [json-args]");
  Deno.exit(2);
}
const r = await call(target.slice(0, dot), target.slice(dot + 1), JSON.parse(json));

// Measure before printing: a result can be megabytes, and only what this
// script prints reaches whoever asked.
const text = typeof r === "string" || r instanceof String ? String(r) : JSON.stringify(r);
console.log(`${text.length} chars`);
console.log(text.slice(0, 500));

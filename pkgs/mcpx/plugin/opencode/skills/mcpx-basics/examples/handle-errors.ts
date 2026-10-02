// Tell a tool's own failure apart from everything else.
//
//   mcpx run handle-errors.ts <namespace>.<tool> '{"arg":"value"}'
//
// A tool that answers isError throws ToolError, carrying the server, the
// tool and the raw result. Anything else -- a bad argument JSON, a network
// failure -- is an ordinary Error. Exits 1 on a tool error so a caller can
// tell.
const [target, json = "{}"] = Deno.args;
const dot = target?.indexOf(".") ?? -1;
if (dot < 1) {
  console.error("usage: mcpx run handle-errors.ts <namespace>.<tool> [json-args]");
  Deno.exit(2);
}
try {
  const r = await call(target.slice(0, dot), target.slice(dot + 1), JSON.parse(json));
  console.log("ok:", JSON.stringify(r).slice(0, 300));
} catch (e) {
  if (e instanceof ToolError) {
    console.log(`tool error from ${e.server}.${e.tool}: ${e.message}`);
    Deno.exit(1);
  }
  throw e;
}

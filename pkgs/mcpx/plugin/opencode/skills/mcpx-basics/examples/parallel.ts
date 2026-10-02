// Run several calls at once and report each outcome.
//
//   mcpx run parallel.ts <namespace>.<tool> '{"a":1}' '{"a":2}' '{"a":3}'
//
// Promise.allSettled rather than Promise.all: one failing call should not
// hide what the others returned. Calls to a shared server run concurrently
// on the daemon; an exclusive one queues them for its lease.
const [target, ...argSets] = Deno.args;
const dot = target?.indexOf(".") ?? -1;
if (dot < 1 || argSets.length === 0) {
  console.error("usage: mcpx run parallel.ts <namespace>.<tool> <json-args> [<json-args>...]");
  Deno.exit(2);
}
const ns = target.slice(0, dot), tool = target.slice(dot + 1);
const started = Date.now();
const results = await Promise.allSettled(argSets.map((a) => call(ns, tool, JSON.parse(a))));
results.forEach((r, i) => {
  const out = r.status === "fulfilled"
    ? "ok    " + JSON.stringify(r.value).slice(0, 120)
    : "error " + (r.reason as Error).message;
  console.log(`${argSets[i]}  ${out}`);
});
console.log(`${results.length} calls in ${Date.now() - started} ms`);

// Read one resource a server publishes.
//
//   mcpx resources                              # every <namespace>/<uri>
//   mcpx run read-resource.ts <namespace> <uri>
//
// readResource() returns the resource's contents; like a tool result, print
// a bounded view rather than the whole thing.
const [ns, uri] = Deno.args;
if (!ns || !uri) {
  console.error("usage: mcpx run read-resource.ts <namespace> <uri>");
  Deno.exit(2);
}
const r = await readResource(ns, uri);
const text = typeof r === "string" || r instanceof String ? String(r) : JSON.stringify(r);
console.log(`${text.length} chars`);
console.log(text.slice(0, 500));

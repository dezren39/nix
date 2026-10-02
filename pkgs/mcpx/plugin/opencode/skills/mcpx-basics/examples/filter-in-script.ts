// Filter inside the script, print only the answer.
//
//   mcpx run filter-in-script.ts <namespace>.<tool> '{"arg":"value"}' [field]
//
// Calls the tool, then reports the result's shape -- type, size, top-level
// keys or array length -- and, when a field is named, that field alone.
// This is the habit that matters: a 200 KB result costs nothing if the
// script prints three lines of it.
const [target, json = "{}", field] = Deno.args;
const [ns, ...rest] = (target ?? "").split(".");
if (!ns || rest.length === 0) {
  console.error("usage: mcpx run filter-in-script.ts <namespace>.<tool> [json-args] [field]");
  Deno.exit(2);
}
const r: any = await call(ns, rest.join("."), JSON.parse(json));
const value = r instanceof String ? String(r) : r;

const shape = Array.isArray(value)
  ? `array of ${value.length}`
  : value !== null && typeof value === "object"
  ? `object with keys ${Object.keys(value).slice(0, 20).join(", ")}`
  : `${typeof value}, ${String(value).length} chars`;
console.log(shape);

if (field !== undefined) {
  const picked = Array.isArray(value) ? value.map((v: any) => v?.[field]) : value?.[field];
  // JSON.stringify(undefined) is undefined, not a string: say so instead.
  console.log(picked === undefined ? `no field ${JSON.stringify(field)}` : JSON.stringify(picked).slice(0, 1000));
}

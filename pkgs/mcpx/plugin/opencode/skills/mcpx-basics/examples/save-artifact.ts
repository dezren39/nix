// Keep a large result as an artifact and print only its reference.
//
//   mcpx run save-artifact.ts <namespace>.<tool> '{"arg":"value"}' [name]
//
// artifact() hands the bytes to the daemon and returns a small reference
// (id, size, sha256, mcpx://artifacts/<id>). Fetch it later with
// `mcpx artifact get <id>`. A tool result carrying an image or audio item can
// be passed straight in; anything else is stored as JSON text here.
const [target, json = "{}", name = "result.json"] = Deno.args;
const dot = target?.indexOf(".") ?? -1;
if (dot < 1) {
  console.error("usage: mcpx run save-artifact.ts <namespace>.<tool> [json-args] [name]");
  Deno.exit(2);
}
const r: any = await call(target.slice(0, dot), target.slice(dot + 1), JSON.parse(json));
const media = r?.raw?.content?.some((c: any) => c.type === "image" || c.type === "audio");
const ref = media
  ? await artifact(name, r)
  : await artifact(name, JSON.stringify(r?.raw ?? r, null, 2), { mime: "application/json" });
console.log(`${ref.name}: ${ref.size} bytes, ${ref.mime}, ${ref.uri}`);

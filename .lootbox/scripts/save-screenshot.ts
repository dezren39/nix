export {};
// Take screenshot and save to file
// Usage: lootbox save-screenshot.ts -- /tmp/filename.png
const outPath = Deno.args[0] || "/tmp/screenshot.png";

await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080" });
// small wait for render
await new Promise((r) => setTimeout(r, 1500));

const r = await tools.mcp_chrome_devtools.take_screenshot({});
const img = r.content.find((c: any) => c.type === "image");
if (img) {
  const bytes = Uint8Array.from(atob(img.data as string), (c: string) =>
    c.charCodeAt(0),
  );
  await Deno.writeFile(outPath, bytes);
  console.log(`Screenshot saved: ${outPath}`);
} else {
  console.error("No image in screenshot response");
}

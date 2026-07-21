export {};
// Take screenshot and output base64 to stdout
// Redirect with: lootbox snap.ts | base64 -d > /tmp/file.png

await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080" });
await new Promise((r) => setTimeout(r, 1500));

const r = await tools.mcp_chrome_devtools.take_screenshot({});
const img = r.content.find((c: any) => c.type === "image");
if (img) {
  console.log(img.data as string);
} else {
  console.error("No image in screenshot response");
}

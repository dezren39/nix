export {};
// Navigate to a URL and take screenshot
const url = Deno.args[0] || "http://localhost:8080";
await tools.mcp_chrome_devtools.navigate_page({ url });
await new Promise((r) => setTimeout(r, 3000));
const r = await tools.mcp_chrome_devtools.take_screenshot({});
const img = r.content.find((c: any) => c.type === "image");
if (img) {
  console.log(img.data as string);
}

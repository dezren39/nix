export {};
// Take screenshot without navigating (captures current state)
const r = await tools.mcp_chrome_devtools.take_screenshot({});
const img = r.content.find((c: any) => c.type === "image");
if (img) {
  console.log(img.data as string);
}

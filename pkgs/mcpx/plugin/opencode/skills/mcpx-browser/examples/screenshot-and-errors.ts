// Open a page, keep a screenshot, and report only the failed requests.
//
//   mcpx run screenshot-and-errors.ts https://example.com [namespace]
//
// The namespace defaults to chrome_devtools; pass yours if you named the
// server differently. Checked against chrome-devtools-mcp, whose tools
// answer in markdown text and take a numeric pageId.
const [url, ns = "chrome_devtools"] = Deno.args;
if (!url) {
  console.error("usage: mcpx run screenshot-and-errors.ts <url> [namespace]");
  Deno.exit(2);
}
const browser = (tools as any)[ns];
if (!browser) {
  console.error(`no namespace ${ns}; there is ${Object.keys(tools).join(", ")}`);
  Deno.exit(2);
}

// new_page answers with the page list; the new page is the [selected] one.
const pages = String(await browser.new_page({ url }));
const selected = pages.split("\n").find((l) => l.endsWith("[selected]"));
const pageId = Number(selected?.match(/^(\d+):/)?.[1]);
if (!Number.isInteger(pageId)) throw new Error("no selected page in:\n" + pages);
console.log("page", selected);

// A screenshot result is a caption and an image; artifact() keeps the image.
const shot = await browser.take_screenshot({ pageId, format: "png" });
const ref = await artifact("page.png", shot);
console.log(`screenshot: ${ref.size} bytes ${ref.mime}, ${ref.uri}`);

// The request list is long; print only what failed.
const net = String(await browser.list_network_requests({ pageId }));
const failed = net.split("\n").filter((l) => /\[(failed|[45]\d\d)\b/i.test(l));
console.log(`${net.split("\n").filter((l) => l.startsWith("reqid=")).length} requests, ${failed.length} failed`);
for (const l of failed.slice(0, 20)) console.log(" ", l);

await browser.close_page({ pageId });

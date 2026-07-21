// @ts-nocheck
// Take snapshots of all key pages for visual audit
const pages = [
  { url: "http://localhost:8080/", name: "portal-home" },
  { url: "http://localhost:8080/irm/", name: "irm-home" },
  { url: "http://localhost:8080/irm/members", name: "irm-members" },
  { url: "http://localhost:8080/irm/incidents", name: "irm-incidents" },
  { url: "http://localhost:8080/irm/readiness", name: "irm-readiness" },
  { url: "http://localhost:8080/admin", name: "portal-admin" },
  { url: "http://localhost:8080/vod", name: "portal-vod" },
];

for (const page of pages) {
  console.log(`\n=== ${page.name}: ${page.url} ===`);
  await tools.mcp_chrome_devtools.navigate_page({ url: page.url });
  await new Promise((r) => setTimeout(r, 1000));
  const snap = await tools.mcp_chrome_devtools.take_snapshot({});
  const content = snap.content;
  if (content) {
    for (let i = 0; i < content.length; i++) {
      const item = content[i];
      if (item && item.type === "text" && typeof item.text === "string") {
        console.log(item.text.substring(0, 3000));
      }
    }
  }
}

// Check console errors
console.log("\n=== Console Messages ===");
const msgs = await tools.mcp_chrome_devtools.list_console_messages({});
const mc = msgs.content;
if (mc) {
  for (let i = 0; i < mc.length; i++) {
    const item = mc[i];
    if (item && item.type === "text" && typeof item.text === "string") {
      console.log(item.text);
    }
  }
}

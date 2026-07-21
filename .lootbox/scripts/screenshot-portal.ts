// Screenshot key portal pages for visual audit
const pages = [
  { url: "http://localhost:8080/", name: "portal-home" },
  { url: "http://localhost:8080/irm/", name: "irm-home" },
  { url: "http://localhost:8080/irm/members", name: "irm-members" },
  { url: "http://localhost:8080/irm/teams", name: "irm-teams" },
  { url: "http://localhost:8080/irm/incidents", name: "irm-incidents" },
  { url: "http://localhost:8080/irm/reports", name: "irm-reports" },
  { url: "http://localhost:8080/irm/readiness", name: "irm-readiness" },
  { url: "http://localhost:8080/admin", name: "portal-admin" },
  { url: "http://localhost:8080/vod", name: "portal-vod" },
];

for (const page of pages) {
  console.log(`\n--- Navigating to ${page.name}: ${page.url} ---`);
  await tools.mcp_chrome_devtools.navigate_page({ url: page.url });
  // Wait a moment for page load
  await new Promise((r) => setTimeout(r, 1500));
  const snapshot = await tools.mcp_chrome_devtools.take_snapshot({});
  console.log(
    `Snapshot for ${page.name}:`,
    JSON.stringify(snapshot).substring(0, 2000),
  );
  const screenshot = await tools.mcp_chrome_devtools.take_screenshot({});
  console.log(
    `Screenshot for ${page.name}:`,
    JSON.stringify(screenshot).substring(0, 500),
  );
}

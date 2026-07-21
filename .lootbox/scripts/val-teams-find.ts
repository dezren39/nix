// val-teams-find.ts — Expand nav menus and find teams page
const BASE = "http://localhost:8080";

console.log("=== Finding Teams Page ===\n");

// Navigate to root
await tools.mcp_chrome_devtools.navigate_page({ url: BASE });
await tools.mcp_chrome_devtools.evaluate_script({
  function: "() => new Promise(r => setTimeout(r, 2000))",
});

// Expand "IRM" disclosure triangle
console.log("[1] Expanding IRM menu...");
await tools.mcp_chrome_devtools.click({ uid: "7_10" });
await tools.mcp_chrome_devtools.evaluate_script({
  function: "() => new Promise(r => setTimeout(r, 1000))",
});
const snap1 = await tools.mcp_chrome_devtools.take_snapshot({});
const snap1Text =
  typeof snap1 === "string" ? snap1 : JSON.stringify(snap1, null, 2);
console.log("IRM expanded:");
console.log(snap1Text.slice(0, 6000));

// Also expand "Incident Readiness"
console.log("\n[2] Expanding Incident Readiness menu...");
// Re-snapshot to get fresh UIDs
const snap1Lines = snap1Text.split("\n");
for (const line of snap1Lines) {
  if (/incident.*readiness|readiness|team|member/i.test(line)) {
    console.log(`  NAV: ${line.trim().slice(0, 200)}`);
  }
}

// Try clicking the Incident Readiness disclosure
await tools.mcp_chrome_devtools.click({ uid: "7_8" });
await tools.mcp_chrome_devtools.evaluate_script({
  function: "() => new Promise(r => setTimeout(r, 1000))",
});
const snap2 = await tools.mcp_chrome_devtools.take_snapshot({});
const snap2Text =
  typeof snap2 === "string" ? snap2 : JSON.stringify(snap2, null, 2);
console.log("\nIncident Readiness expanded:");
const snap2Lines = snap2Text.split("\n");
for (const line of snap2Lines) {
  if (/team|member|readiness|link|url/i.test(line)) {
    console.log(`  ${line.trim().slice(0, 200)}`);
  }
}

// Now try navigating directly to potential URLs
console.log("\n[3] Probing URLs via fetch...");
const probePaths = [
  "/irm/",
  "/irm/teams",
  "/irm/team",
  "/irm/members",
  "/incident-readiness",
  "/incident-readiness/teams",
  "/incident-readiness/members",
  "/incident-readiness/team",
];
for (const p of probePaths) {
  const resp = await tools.mcp_chrome_devtools.navigate_page({
    url: `${BASE}${p}`,
  });
  await tools.mcp_chrome_devtools.evaluate_script({
    function: "() => new Promise(r => setTimeout(r, 500))",
  });
  const s = await tools.mcp_chrome_devtools.take_snapshot({});
  const sText = typeof s === "string" ? s : JSON.stringify(s, null, 2);
  const has404 = sText.includes("Not Found") || sText.includes("404");
  const hasTeam = /team/i.test(sText);
  const hasGrid = /ag-grid|ag-theme|<table|<th/i.test(sText);
  console.log(
    `  ${p} => 404=${has404}, hasTeam=${hasTeam}, hasGrid=${hasGrid}`,
  );
  if (!has404 && hasTeam) {
    console.log(`    *** FOUND TEAMS CONTENT at ${p} ***`);
    console.log(sText.slice(0, 3000));
  }
}

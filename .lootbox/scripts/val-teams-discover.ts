// val-teams-discover.ts — Find the correct teams page URL
const BASE = "http://localhost:8080";

console.log("=== Discovering Teams Page URL ===\n");

// First check the root page for navigation links
console.log("[1] Navigating to root...");
await tools.mcp_chrome_devtools.navigate_page({ url: BASE });
await tools.mcp_chrome_devtools.evaluate_script({
  function: "() => new Promise(r => setTimeout(r, 2000))",
});

console.log("[2] Taking root snapshot...");
const snap = await tools.mcp_chrome_devtools.take_snapshot({});
const snapText =
  typeof snap === "string" ? snap : JSON.stringify(snap, null, 2);
console.log(snapText.slice(0, 12000));

// Look for links containing "team" in any form
console.log("\n[3] Searching for team-related links...");
const teamRe = /team/gi;
const lines = snapText.split("\n");
for (const line of lines) {
  if (teamRe.test(line)) {
    console.log(`  ${line.trim().slice(0, 300)}`);
    teamRe.lastIndex = 0; // reset for next test
  }
}

// Also try common paths
console.log("\n[4] Trying common team page paths...");
const paths = [
  "/teams",
  "/team",
  "/api/teams",
  "/pages/teams",
  "/#/teams",
  "/#teams",
];
for (const p of paths) {
  const resp = await tools.mcp_chrome_devtools.evaluate_script({
    function: `(path) => fetch(path).then(r => r.status + ' ' + r.statusText).catch(e => 'ERR: ' + e.message)`,
    args: [p],
  });
  const respText = typeof resp === "string" ? resp : JSON.stringify(resp);
  console.log(`  ${p} => ${respText.slice(0, 200)}`);
}

// @ts-nocheck
// Phase 19.7.A.4: Console Errors — Part 1 (home, incidents, teams, members)
const cd = tools.mcp_chrome_devtools;
function text(r: any): string {
  return r.content.map((c: any) => c.text ?? "").join("");
}

const DIR =
  "/Users/drewry.pope/.config/nix/.opencode/worktrees/theme-align/features/2026-04-06_0019.0_theme-alignment-bootstrap-removal/screenshots";
const PAGES = [
  { url: "http://localhost:8080/", name: "home" },
  { url: "http://localhost:8080/irm/incidents", name: "incidents" },
  { url: "http://localhost:8080/irm/teams", name: "teams" },
  { url: "http://localhost:8080/irm/members", name: "members" },
];

console.log("=== Console Errors Part 1 ===\n");

for (const page of PAGES) {
  try {
    await cd.navigate_page({ url: page.url });
    await cd.take_screenshot({
      filePath: `${DIR}/E05-console-${page.name}.png`,
    });
    console.log(`${page.name}: screenshot OK`);
  } catch (e: any) {
    console.log(`${page.name}: FAILED — ${e?.message || e}`);
  }
}

// Collect all errors after visiting pages
try {
  const msgs = await cd.list_console_messages({ types: ["error", "warn"] });
  const raw = text(msgs);
  console.log("\n--- Console Messages ---");
  console.log(raw.substring(0, 800));
} catch (e: any) {
  console.log(`Console fetch failed: ${e?.message || e}`);
}
console.log("\nPart 1 done.");

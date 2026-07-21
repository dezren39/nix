// @ts-nocheck
// Phase 19.7.A.4: Console Errors — Part 2 (readiness, vod, admin) + Summary
const cd = tools.mcp_chrome_devtools;
function text(r: any): string {
  return r.content.map((c: any) => c.text ?? "").join("");
}

const DIR =
  "/Users/drewry.pope/.config/nix/.opencode/worktrees/theme-align/features/2026-04-06_0019.0_theme-alignment-bootstrap-removal/screenshots";
const PAGES = [
  { url: "http://localhost:8080/readiness", name: "readiness" },
  { url: "http://localhost:8080/vod", name: "vod" },
  { url: "http://localhost:8080/admin", name: "admin" },
];

console.log("=== Console Errors Part 2 ===\n");

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

try {
  const msgs = await cd.list_console_messages({ types: ["error", "warn"] });
  const raw = text(msgs);
  console.log("\n--- Console Messages (all pages) ---");
  console.log(raw.substring(0, 1000));
} catch (e: any) {
  console.log(`Console fetch failed: ${e?.message || e}`);
}

console.log("\n=== Console Errors OVERALL: Check messages above ===");
console.log(
  "Total pages: 7 (home, incidents, teams, members, readiness, vod, admin)",
);
console.log("Screenshots: E05-console-*.png");

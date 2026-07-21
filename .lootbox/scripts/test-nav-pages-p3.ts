// @ts-nocheck
// Phase 19.7.C — Navigation Part 3 (C11-C14) + Console Errors
const cd = tools.mcp_chrome_devtools;
function text(r: any): string {
  return r.content.map((c: any) => c.text ?? "").join("");
}
async function evalJs(fn: string): Promise<any> {
  const r = await cd.evaluate_script({ function: fn });
  const raw = text(r);
  const m = raw.match(/```(?:json)?\n([\s\S]*?)\n```/);
  try {
    return JSON.parse(m ? m[1] : raw);
  } catch {
    return m ? m[1] : raw;
  }
}
const DIR =
  "/Users/drewry.pope/.config/nix/.opencode/worktrees/theme-align/features/2026-04-06_0019.0_theme-alignment-bootstrap-removal/screenshots";
const pages = [
  {
    id: "C11",
    path: "/vod/aws-postgresql",
    url: "http://localhost:8080/vod/aws-postgresql",
    file: "C11-vod-aws-postgresql.png",
  },
  {
    id: "C12",
    path: "/readiness",
    url: "http://localhost:8080/readiness",
    file: "C12-readiness.png",
  },
  {
    id: "C13",
    path: "/readiness/dashboard",
    url: "http://localhost:8080/readiness/dashboard",
    file: "C13-readiness-dashboard.png",
  },
  {
    id: "C14",
    path: "/irm",
    url: "http://localhost:8080/irm",
    file: "C14-irm-home.png",
  },
];
console.log("=== Nav Part 3 (C11-C14) ===");
for (const page of pages) {
  try {
    await cd.navigate_page({ url: page.url });
    await cd.take_screenshot({ filePath: `${DIR}/${page.file}` });
    const title = await evalJs("() => document.title");
    console.log(`${page.id} ${page.path}: ${title} — OK`);
  } catch (err: any) {
    console.log(`${page.id} ${page.path}: FAILED — ${err?.message || err}`);
  }
}
console.log("\n--- Console Errors ---");
try {
  const msgs = await cd.list_console_messages({ types: ["error"] });
  const raw = text(msgs);
  console.log(raw.substring(0, 500));
} catch (err: any) {
  console.log(`Failed: ${err?.message || err}`);
}
console.log("\nAll parts complete.");

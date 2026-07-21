// @ts-nocheck
// Phase 19.7.C — Navigation Part 2 (C06-C10)
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
    id: "C06",
    path: "/reports",
    url: "http://localhost:8080/reports",
    file: "C06-reports.png",
  },
  {
    id: "C07",
    path: "/admin",
    url: "http://localhost:8080/admin",
    file: "C07-admin.png",
  },
  {
    id: "C08",
    path: "/vod",
    url: "http://localhost:8080/vod",
    file: "C08-vod-overview.png",
  },
  {
    id: "C09",
    path: "/vod/rings",
    url: "http://localhost:8080/vod/rings",
    file: "C09-vod-rings.png",
  },
  {
    id: "C10",
    path: "/vod/migration",
    url: "http://localhost:8080/vod/migration",
    file: "C10-vod-migration.png",
  },
];
console.log("=== Nav Part 2 (C06-C10) ===");
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
console.log("Part 2 done.");

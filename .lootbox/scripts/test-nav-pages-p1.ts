// @ts-nocheck
// Phase 19.7.C — Navigation Part 1 (C01-C05)
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
  { id: "C01", path: "/", url: "http://localhost:8080/", file: "C01-home.png" },
  {
    id: "C02",
    path: "/irm/incidents",
    url: "http://localhost:8080/irm/incidents",
    file: "C02-incidents.png",
  },
  {
    id: "C03",
    path: "/irm/teams",
    url: "http://localhost:8080/irm/teams",
    file: "C03-teams.png",
  },
  {
    id: "C04",
    path: "/irm/members",
    url: "http://localhost:8080/irm/members",
    file: "C04-members.png",
  },
  {
    id: "C05",
    path: "/pages",
    url: "http://localhost:8080/pages",
    file: "C05-pages-list.png",
  },
];
console.log("=== Nav Part 1 (C01-C05) ===");
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
console.log("Part 1 done.");

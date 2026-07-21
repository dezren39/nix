// @ts-nocheck
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
async function wait(ms: number): Promise<void> {
  await evalJs(
    `async () => { await new Promise(r => setTimeout(r, ${ms})); return "waited"; }`,
  );
}

const SCREENSHOT_DIR =
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

const results: any[] = [];
let screenshotCount = 0;

console.log("=== Phase 19.7.C — Navigation Screenshot Inventory ===\n");

for (const page of pages) {
  try {
    await cd.navigate_page({ url: page.url });
    await wait(300);

    const filePath = `${SCREENSHOT_DIR}/${page.file}`;
    await cd.take_screenshot({ filePath });
    screenshotCount++;

    const title = await evalJs("() => document.title");

    const pageError = await evalJs(
      "() => document.querySelector('.alert-error')?.textContent || null",
    );

    if (pageError) {
      console.log(`${page.id} ${page.path}: ERROR — ${pageError}`);
      results.push({
        id: page.id,
        path: page.path,
        title,
        status: "error",
        error: pageError,
      });
    } else {
      console.log(`${page.id} ${page.path}: ${title} — OK`);
      results.push({ id: page.id, path: page.path, title, status: "ok" });
    }
  } catch (err: any) {
    const msg = err?.message || String(err);
    console.log(`${page.id} ${page.path}: FAILED — ${msg}`);
    results.push({
      id: page.id,
      path: page.path,
      title: "",
      status: "failed",
      error: msg,
    });
  }
}

// Check console errors
console.log("\n--- Console Errors ---");
let consoleErrorCount = 0;
try {
  const msgs = await cd.list_console_messages({ types: ["error"] });
  const raw = text(msgs);
  if (raw && raw.trim().length > 0) {
    console.log(raw);
    const lines = raw.split("\n").filter((l: string) => l.trim().length > 0);
    consoleErrorCount = lines.length;
  } else {
    console.log("(none)");
  }
} catch (err: any) {
  console.log(`Failed to fetch console errors: ${err?.message || err}`);
}

// Summary
const failed = results.filter((r: any) => r.status === "failed");
const errored = results.filter((r: any) => r.status === "error");

console.log("\n=== Summary ===");
console.log(`Screenshots taken: ${screenshotCount} / ${pages.length}`);
console.log(
  `Pages OK:          ${results.filter((r: any) => r.status === "ok").length}`,
);
console.log(`Pages with errors: ${errored.length}`);
console.log(`Pages failed:      ${failed.length}`);
console.log(`Console errors:    ${consoleErrorCount}`);

if (errored.length > 0) {
  console.log("\nPages with errors:");
  for (const r of errored) {
    console.log(`  ${r.id} ${r.path}: ${r.error}`);
  }
}

if (failed.length > 0) {
  console.log("\nFailed pages:");
  for (const r of failed) {
    console.log(`  ${r.id} ${r.path}: ${r.error}`);
  }
}

console.log(`\nScreenshots saved to:\n  ${SCREENSHOT_DIR}/`);

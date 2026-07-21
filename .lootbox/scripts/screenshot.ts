// Take all screenshots for upstream merge report
const BASE = "http://localhost:8080";
const DIR =
  "/Users/drewry.pope/git/incident-response-management/feature/upstream-merge-2026-04-07";

const pages = [
  {
    url: `${BASE}/`,
    name: "01-homepage.png",
    desc: "Homepage with sidebar nav",
  },
  {
    url: `${BASE}/vod`,
    name: "02-vod-overview.png",
    desc: "VOD overview with account name links",
  },
  {
    url: `${BASE}/vod/aws-postgresql`,
    name: "03-vod-aws-postgresql.png",
    desc: "VOD AWS PostgreSQL with account links",
  },
  {
    url: `${BASE}/vod/migration`,
    name: "04-vod-migration.png",
    desc: "VOD migration with account links",
  },
  {
    url: `${BASE}/vod/rings`,
    name: "05-vod-rings.png",
    desc: "VOD rings with account links",
  },
];

const results: { name: string; desc: string; ok: boolean; error?: string }[] =
  [];

for (const page of pages) {
  try {
    console.log(`Navigating to ${page.url}...`);
    await tools.mcp_chrome_devtools.navigate_page({ url: page.url });
    // Wait for render
    await new Promise((r) => setTimeout(r, 2000));
    const r = await tools.mcp_chrome_devtools.take_screenshot({
      filePath: `${DIR}/${page.name}`,
      fullPage: true,
    });
    console.log(`OK: ${page.name}`);
    results.push({ name: page.name, desc: page.desc, ok: true });
  } catch (e: any) {
    console.error(`FAIL: ${page.name} - ${e.message}`);
    results.push({
      name: page.name,
      desc: page.desc,
      ok: false,
      error: e.message,
    });
  }
}

console.log("\n=== Results ===");
console.log(JSON.stringify(results, null, 2));

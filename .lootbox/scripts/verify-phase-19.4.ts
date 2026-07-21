// Phase 19.4.16 — Verify all migrated portal templates render with DaisyUI

const pages = [
  { name: "Portal Home", url: "http://localhost:8080/" },
  { name: "Admin", url: "http://localhost:8080/admin" },
  { name: "IRM Home", url: "http://localhost:8080/irm/" },
  { name: "Incident Org", url: "http://localhost:8080/incident-readiness" },
  { name: "VOD Overview", url: "http://localhost:8080/vod/overview" },
  { name: "VOD Migration", url: "http://localhost:8080/vod/oci-migration" },
  { name: "VOD Rings", url: "http://localhost:8080/vod/deployment-rings" },
  { name: "VOD AWS PG", url: "http://localhost:8080/vod/aws-postgresql" },
];

function getTextContent(result: {
  content: Array<{ type: string; text?: string }>;
}): string {
  return result.content.map((c) => c.text || "").join("\n");
}

let allPassed = true;

for (const page of pages) {
  console.log(`\n=== ${page.name} (${page.url}) ===`);

  await tools.mcp_chrome_devtools.navigate_page({ url: page.url });

  // Wait for content
  await tools.mcp_chrome_devtools
    .wait_for({ text: ["Operations Portal"], timeout: 5000 })
    .catch(() => {
      console.log("WARN: sidebar text not found in 5s");
    });

  const snap = await tools.mcp_chrome_devtools.take_snapshot({});
  const html = getTextContent(snap);

  // Key checks
  const hasSidebar = html.includes("Operations Portal");
  const hasBootstrapCDN = html.includes("cdn.jsdelivr.net/npm/bootstrap");
  const hasBootstrapClasses =
    /\bcontainer-fluid\b|\bd-flex\b|\btext-muted\b|\bform-control\b|\btable-striped\b|\bbg-light\b/.test(
      html,
    );
  const hasDaisyUI = html.includes("data-theme") || html.includes("bg-base");
  const hasContent = html.length > 500; // Page has actual content, not empty

  const pass = hasSidebar && !hasBootstrapCDN && hasDaisyUI && hasContent;

  console.log(`  Has sidebar: ${hasSidebar ? "OK" : "FAIL"}`);
  console.log(`  No Bootstrap CDN: ${!hasBootstrapCDN ? "OK" : "FAIL"}`);
  console.log(
    `  No Bootstrap classes in snapshot: ${!hasBootstrapClasses ? "OK" : "WARN (may be in text content)"}`,
  );
  console.log(`  DaisyUI markers: ${hasDaisyUI ? "OK" : "FAIL"}`);
  console.log(
    `  Has content (${html.length} chars): ${hasContent ? "OK" : "FAIL"}`,
  );
  console.log(`  Overall: ${pass ? "PASS" : "FAIL"}`);

  if (!pass) allPassed = false;

  // Take screenshot for visual review
  await tools.mcp_chrome_devtools.take_screenshot({});
  console.log(`  Screenshot taken`);
}

console.log(
  `\n=== Phase 19.4.16 verification: ${allPassed ? "ALL PASS" : "SOME FAILURES — check above"} ===`,
);

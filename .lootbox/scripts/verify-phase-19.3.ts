// Phase 19.3.7 — Verify DaisyUI sidebar, theme picker, light/dark toggle

const pages = [
  { name: "Portal Home", url: "http://localhost:8080/" },
  { name: "IRM Home", url: "http://localhost:8080/irm/" },
  { name: "Admin", url: "http://localhost:8080/admin" },
];

function getTextContent(result: {
  content: Array<{ type: string; text?: string }>;
}): string {
  return result.content.map((c) => c.text || "").join("\n");
}

for (const page of pages) {
  console.log(`\n=== ${page.name} (${page.url}) ===`);

  await tools.mcp_chrome_devtools.navigate_page({ url: page.url });

  // Wait for page content to render
  await tools.mcp_chrome_devtools
    .wait_for({ text: ["Operations Portal"], timeout: 5000 })
    .catch(() => {
      console.log("WARN: 'Operations Portal' text not found within 5s");
    });

  // Take snapshot to check markup
  const snap = await tools.mcp_chrome_devtools.take_snapshot({});
  const html = getTextContent(snap);

  // Check for DaisyUI markers
  const hasSidebar =
    html.includes("menu") && html.includes("Operations Portal");
  const hasThemePicker =
    html.includes("theme-picker-container") || html.includes("theme-label");
  const hasModeToggle = html.includes("swap") || html.includes("mode-toggle");
  const hasBootstrap = html.includes("cdn.jsdelivr.net/npm/bootstrap");
  const hasDataTheme = html.includes("data-theme");

  console.log(`  DaisyUI sidebar: ${hasSidebar ? "✅" : "❌"}`);
  console.log(`  Theme picker container: ${hasThemePicker ? "✅" : "❌"}`);
  console.log(`  Mode toggle: ${hasModeToggle ? "✅" : "❌"}`);
  console.log(`  data-theme: ${hasDataTheme ? "✅" : "❌"}`);
  console.log(
    `  No Bootstrap CDN: ${!hasBootstrap ? "✅" : "❌ (Bootstrap still found!)"}`,
  );

  // Check console for errors
  const consoleMsgs = await tools.mcp_chrome_devtools.list_console_messages({});
  const consoleText = getTextContent(consoleMsgs);
  const hasErrors =
    consoleText.toLowerCase().includes('"error"') ||
    consoleText.toLowerCase().includes("level: error");
  console.log(
    `  Console errors: ${!hasErrors ? "✅ none detected" : "⚠️ possible errors — check manually"}`,
  );
}

console.log("\n=== Phase 19.3.7 verification complete ===");

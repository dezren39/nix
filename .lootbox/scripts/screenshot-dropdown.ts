/**
 * Takes a screenshot of the theme dropdown in its open/overlay state.
 * @example lootbox screenshot-dropdown.ts
 */

const cd = tools.mcp_chrome_devtools;

function text(r: { content: Array<{ type: string; text?: string }> }): string {
  return r.content.map((c) => c.text ?? "").join("");
}

// Navigate and wait for page
await cd.navigate_page({ url: "http://localhost:8080" });
await cd.wait_for({ text: ["Theme"], timeout: 5000 });

// Find the theme label uid from accessibility snapshot
const snap = await cd.take_snapshot({});
const snapText = text(snap);
const labelMatch = snapText.match(/uid=(\S+)\s+LabelText/);
if (!labelMatch) {
  console.log("ERROR: Could not find theme LabelText in snapshot");
} else {
  // Click to open dropdown
  await cd.click({ uid: labelMatch[1] });
  await new Promise((r) => setTimeout(r, 500));

  // Take screenshot with dropdown open
  const shot = await cd.take_screenshot({
    filePath:
      "/Users/drewry.pope/git/incident-response-management/dropdown-overlay.png",
  });
  console.log(text(shot));
  console.log("\nScreenshot saved to dropdown-overlay.png");
}

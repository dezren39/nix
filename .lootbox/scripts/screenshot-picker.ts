// Screenshot the IRM theme picker in its open state
// Uses #sidebar-toggle checkbox (peer-checked:translate-x-0 pattern)
// to show sidebar at mobile viewport (800x600)

const APP_URL = "http://localhost:8080/";
const SCREENSHOT_PATH =
  "/Users/drewry.pope/.config/nix/.opencode/worktrees/integration-irm/theme-picker-open.png";

// Step 1: Navigate to the app
console.log("Step 1: Navigating to", APP_URL);
await tools.mcp_chrome_devtools.navigate_page({ url: APP_URL, timeout: 15000 });

// Step 2: Wait for page to load
console.log("Step 2: Waiting for page load...");
await new Promise((r) => setTimeout(r, 2000));

// Step 3: Open sidebar via the peer checkbox (#sidebar-toggle)
// At 800x600 the aside uses -translate-x-full (offscreen) and
// peer-checked:translate-x-0 (onscreen). Checking the hidden
// checkbox triggers the Tailwind peer selector.
console.log("Step 3: Opening sidebar via #sidebar-toggle...");
const sidebarResult = await tools.mcp_chrome_devtools.evaluate_script({
  function: `() => {
    const cb = document.getElementById("sidebar-toggle");
    if (!cb) return { error: "sidebar-toggle not found" };
    cb.checked = true;
    cb.dispatchEvent(new Event("change", { bubbles: true }));

    const aside = document.querySelector("aside");
    // Clear any stale inline styles from previous runs
    if (aside) aside.style.cssText = "";

    const rect = aside?.getBoundingClientRect();
    return { checked: cb.checked, asideX: rect?.x, asideWidth: rect?.width };
  }`,
});
console.log("Sidebar:", JSON.stringify(sidebarResult).substring(0, 300));

await new Promise((r) => setTimeout(r, 300));

// Step 4: Click the theme picker trigger button to open the dropdown
console.log("Step 4: Clicking theme picker trigger...");
const clickResult = await tools.mcp_chrome_devtools.evaluate_script({
  function: `() => {
    const btn = document.querySelector("#theme-picker-container button");
    if (!btn) return { clicked: false, error: "theme picker button not found" };
    btn.click();
    return { clicked: true, text: btn.textContent.trim().substring(0, 40) };
  }`,
});
console.log("Click:", JSON.stringify(clickResult).substring(0, 200));

// Step 5: Wait for dropdown animation
console.log("Step 5: Waiting for animation...");
await new Promise((r) => setTimeout(r, 800));

// Step 6: Take screenshot
console.log("Step 6: Taking screenshot...");
const screenshot = await tools.mcp_chrome_devtools.take_screenshot({
  filePath: SCREENSHOT_PATH,
  fullPage: true,
  format: "png",
});
console.log("Screenshot:", JSON.stringify(screenshot).substring(0, 300));

// Step 7: Verify file
try {
  const stat = await Deno.stat(SCREENSHOT_PATH);
  console.log(`\nSUCCESS: ${SCREENSHOT_PATH} (${stat.size} bytes)`);
} catch {
  console.log("\nWARNING: Could not verify file at", SCREENSHOT_PATH);
}

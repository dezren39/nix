// validate-responsive.ts - Responsive design validation for Operations Portal
// Takes screenshots at desktop and mobile widths, checks hamburger visibility,
// navigates to members page at mobile, and lists console errors.

const BASE = "http://localhost:8080";
const SCREENSHOTS_DIR =
  "/Users/drewry.pope/.config/nix/.opencode/worktrees/integration-irm/.lootbox/screenshots";

// Ensure screenshots dir exists
await tools.mcp_chrome_devtools.evaluate_script({
  function: `(() => "ready")`,
});

// Step 1: Navigate to home page at full/desktop width
console.log("=== Step 1: Navigate to home at desktop width ===");
await tools.mcp_chrome_devtools.navigate_page({
  url: BASE + "/irm/",
  timeout: 15000,
});

// Take desktop screenshot
console.log("Taking desktop screenshot...");
const desktopShot = await tools.mcp_chrome_devtools.take_screenshot({
  fullPage: true,
  filePath: SCREENSHOTS_DIR + "/responsive-desktop.png",
});
console.log(
  "Desktop screenshot:",
  JSON.stringify(desktopShot).substring(0, 200),
);

// Take a snapshot to see DOM structure at desktop
console.log("\n=== Step 2: Check desktop layout ===");
const desktopSnap = await tools.mcp_chrome_devtools.take_snapshot({});
console.log(
  "Desktop snapshot (first 2000 chars):",
  JSON.stringify(desktopSnap).substring(0, 2000),
);

// Step 3: Resize to mobile width (375x812 = iPhone SE)
console.log("\n=== Step 3: Resize to mobile (375x812) ===");
await tools.mcp_chrome_devtools.evaluate_script({
  function: `(() => {
    // Use CDP-style resize via window manipulation
    window.resizeTo(375, 812);
    // Also set innerWidth for media query triggers
    return 'Resized to 375x812. innerWidth=' + window.innerWidth;
  })`,
});

// Wait for CSS media queries to apply
await new Promise((resolve) => setTimeout(resolve, 1000));

// Take mobile screenshot
console.log("Taking mobile screenshot...");
const mobileShot = await tools.mcp_chrome_devtools.take_screenshot({
  fullPage: true,
  filePath: SCREENSHOTS_DIR + "/responsive-mobile-home.png",
});
console.log("Mobile screenshot:", JSON.stringify(mobileShot).substring(0, 200));

// Take snapshot at mobile to check hamburger visibility
console.log("\n=== Step 4: Check hamburger/sidebar toggle at mobile ===");
const mobileSnap = await tools.mcp_chrome_devtools.take_snapshot({});
console.log(
  "Mobile snapshot (first 3000 chars):",
  JSON.stringify(mobileSnap).substring(0, 3000),
);

// Check if the hamburger button (label for sidebar-toggle) is visible
const hamburgerCheck = await tools.mcp_chrome_devtools.evaluate_script({
  function: `(() => {
    const hamburger = document.querySelector('label[for="sidebar-toggle"]');
    if (!hamburger) return 'HAMBURGER NOT FOUND IN DOM';
    const style = window.getComputedStyle(hamburger);
    const rect = hamburger.getBoundingClientRect();
    return JSON.stringify({
      exists: true,
      display: style.display,
      visibility: style.visibility,
      opacity: style.opacity,
      width: rect.width,
      height: rect.height,
      top: rect.top,
      left: rect.left,
      classes: hamburger.className,
      windowInnerWidth: window.innerWidth,
      windowInnerHeight: window.innerHeight,
    });
  })`,
});
console.log("Hamburger visibility check:", JSON.stringify(hamburgerCheck));

// Check sidebar visibility at mobile
const sidebarCheck = await tools.mcp_chrome_devtools.evaluate_script({
  function: `(() => {
    const sidebar = document.querySelector('aside');
    if (!sidebar) return 'SIDEBAR NOT FOUND';
    const style = window.getComputedStyle(sidebar);
    const rect = sidebar.getBoundingClientRect();
    return JSON.stringify({
      display: style.display,
      transform: style.transform,
      position: style.position,
      left: rect.left,
      width: rect.width,
      translateXHidden: rect.left < 0 || style.transform.includes('translateX'),
      windowInnerWidth: window.innerWidth,
    });
  })`,
});
console.log("Sidebar visibility check:", JSON.stringify(sidebarCheck));

// Check if main content reflows properly
const mainContentCheck = await tools.mcp_chrome_devtools.evaluate_script({
  function: `(() => {
    const main = document.getElementById('main-content');
    if (!main) return 'MAIN NOT FOUND';
    const style = window.getComputedStyle(main);
    const rect = main.getBoundingClientRect();
    return JSON.stringify({
      width: rect.width,
      padding: style.padding,
      overflowX: style.overflowX,
      windowInnerWidth: window.innerWidth,
    });
  })`,
});
console.log("Main content at mobile:", JSON.stringify(mainContentCheck));

// Step 5: Navigate to members page at mobile width
console.log("\n=== Step 5: Navigate to members page at mobile ===");
await tools.mcp_chrome_devtools.navigate_page({
  url: BASE + "/irm/members",
  timeout: 15000,
});

// Wait for page load
await new Promise((resolve) => setTimeout(resolve, 1500));

// Take members page mobile screenshot
const membersMobileShot = await tools.mcp_chrome_devtools.take_screenshot({
  fullPage: true,
  filePath: SCREENSHOTS_DIR + "/responsive-mobile-members.png",
});
console.log(
  "Members mobile screenshot:",
  JSON.stringify(membersMobileShot).substring(0, 200),
);

// Check members grid at mobile
const membersGridCheck = await tools.mcp_chrome_devtools.evaluate_script({
  function: `(() => {
    const grid = document.getElementById('members-grid');
    if (!grid) return 'GRID NOT FOUND';
    const rect = grid.getBoundingClientRect();
    const main = document.getElementById('main-content');
    const mainRect = main ? main.getBoundingClientRect() : null;
    return JSON.stringify({
      gridWidth: rect.width,
      gridOverflows: rect.width > window.innerWidth,
      mainWidth: mainRect ? mainRect.width : null,
      windowInnerWidth: window.innerWidth,
      hasHorizontalScroll: document.body.scrollWidth > window.innerWidth,
    });
  })`,
});
console.log("Members grid at mobile:", JSON.stringify(membersGridCheck));

// Step 6: List console errors
console.log("\n=== Step 6: Console errors ===");
const consoleMessages = await tools.mcp_chrome_devtools.list_console_messages({
  types: ["error", "warning"],
  pageSize: 50,
});
console.log(
  "Console errors/warnings:",
  JSON.stringify(consoleMessages, null, 2),
);

console.log("\n=== Responsive validation complete ===");

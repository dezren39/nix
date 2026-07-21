// Theme picker test - use existing page, click uid=1_16 (Change theme button)
// The headless chrome viewport is 800x600 but the sidebar exists.
// First, let's check the current page and use evaluate_script to resize viewport.

// Step 1: Get page list
const pages = await tools.mcp_chrome_devtools.list_pages({});
console.log("Pages:", JSON.stringify(pages));

// Step 2: Use navigate_page with initScript to attempt viewport change
// Actually let's just use the existing page and try clicking the theme button via JS
// since the uid-based click may not work if the element is off-screen

// Step 3: Click via evaluate_script to trigger theme picker
const clickResult = await tools.mcp_chrome_devtools.evaluate_script({
  function: `() => {
    const container = document.getElementById('theme-picker-container');
    const btn = container?.querySelector('button');
    if (btn) {
      btn.click();
      // Wait a moment and check what happened
      return {
        clicked: true,
        btnText: btn.textContent?.trim(),
        containerChildCount: container?.children?.length,
        containerHTML: container?.innerHTML?.substring(0, 1000)
      };
    }
    return { clicked: false, error: 'no button found' };
  }`,
});
console.log("=== CLICK RESULT ===");
console.log(JSON.stringify(clickResult, null, 2));

// Wait for panel to appear
await new Promise((r) => setTimeout(r, 500));

// Step 4: Check panel state
const panelState = await tools.mcp_chrome_devtools.evaluate_script({
  function: `() => {
    const container = document.getElementById('theme-picker-container');
    if (!container) return { error: 'no container' };

    // Check all children for visibility
    const children = Array.from(container.children);
    const childInfo = children.map((c, i) => {
      const rect = c.getBoundingClientRect();
      const style = window.getComputedStyle(c);
      return {
        idx: i,
        tag: c.tagName,
        className: c.className?.substring(0, 100),
        display: style.display,
        visibility: style.visibility,
        height: c.offsetHeight,
        width: c.offsetWidth,
        top: Math.round(rect.top),
        left: Math.round(rect.left)
      };
    });

    return {
      totalChildren: children.length,
      children: childInfo,
      containerHeight: container.offsetHeight,
      containerWidth: container.offsetWidth,
      containerRect: {
        top: Math.round(container.getBoundingClientRect().top),
        left: Math.round(container.getBoundingClientRect().left),
        width: Math.round(container.getBoundingClientRect().width),
        height: Math.round(container.getBoundingClientRect().height)
      }
    };
  }`,
});
console.log("=== PANEL STATE ===");
console.log(JSON.stringify(panelState, null, 2));

// Step 5: Take snapshot to see the panel in accessibility tree
const snap = await tools.mcp_chrome_devtools.take_snapshot({});
console.log("=== SNAPSHOT (after click) ===");
const snapText = JSON.stringify(snap);
console.log(snapText.substring(0, 4000));

// Step 6: Take screenshot
const ss = await tools.mcp_chrome_devtools.take_screenshot({});
console.log("=== SCREENSHOT ===");
console.log(JSON.stringify(ss).substring(0, 200));
// Screenshot is base64 image, truncate for readability

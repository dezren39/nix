// Check if theme picker panel actually opens after JS click
// Focus on the panel display state

// Click the trigger button
const click = await tools.mcp_chrome_devtools.evaluate_script({
  function: `() => {
    const container = document.getElementById('theme-picker-container');
    const dropdown = container?.querySelector('.dropdown');
    const btn = dropdown?.querySelector('button');
    const panel = dropdown?.querySelector('.dropdown-content');
    
    // Before state
    const before = {
      panelDisplay: panel?.style.display,
      panelCompDisplay: panel ? window.getComputedStyle(panel).display : null,
      dropdownOpen: dropdown?.classList.contains('dropdown-open'),
      panelHeight: panel?.offsetHeight
    };
    
    // Simulate click on the trigger button
    if (btn) btn.click();
    
    // After state (immediate)
    const after = {
      panelDisplay: panel?.style.display,
      panelCompDisplay: panel ? window.getComputedStyle(panel).display : null,
      dropdownOpen: dropdown?.classList.contains('dropdown-open'),
      panelHeight: panel?.offsetHeight,
      panelChildren: panel?.children.length,
      panelRect: panel ? {
        top: Math.round(panel.getBoundingClientRect().top),
        left: Math.round(panel.getBoundingClientRect().left),
        width: Math.round(panel.getBoundingClientRect().width),
        height: Math.round(panel.getBoundingClientRect().height)
      } : null
    };
    
    return { before, after };
  }`,
});
console.log("=== CLICK TOGGLE STATE ===");
console.log(JSON.stringify(click, null, 2));

// Wait a bit for async renders
await new Promise((r) => setTimeout(r, 300));

// Check again after async renders
const afterAsync = await tools.mcp_chrome_devtools.evaluate_script({
  function: `() => {
    const container = document.getElementById('theme-picker-container');
    const dropdown = container?.querySelector('.dropdown');
    const panel = dropdown?.querySelector('.dropdown-content');
    
    return {
      panelDisplay: panel?.style.display,
      panelCompDisplay: panel ? window.getComputedStyle(panel).display : null,
      dropdownOpen: dropdown?.classList.contains('dropdown-open'),
      panelHeight: panel?.offsetHeight,
      panelWidth: panel?.offsetWidth,
      panelChildren: panel?.children.length,
      panelRect: panel ? {
        top: Math.round(panel.getBoundingClientRect().top),
        left: Math.round(panel.getBoundingClientRect().left),
        width: Math.round(panel.getBoundingClientRect().width),
        height: Math.round(panel.getBoundingClientRect().height)
      } : null,
      // Check for list items
      listArea: panel ? panel.querySelector('.overflow-y-auto')?.innerHTML?.substring(0, 500) : null
    };
  }`,
});
console.log("=== AFTER ASYNC RENDER ===");
console.log(JSON.stringify(afterAsync, null, 2));

// Now take a screenshot - note the sidebar is at x=-240 so we need to scroll to it
// or use evaluate_script to temporarily move it on-screen
const moveAndShot = await tools.mcp_chrome_devtools.evaluate_script({
  function: `() => {
    const sidebar = document.querySelector('aside');
    if (sidebar) {
      sidebar.style.transform = 'translateX(0)';
      sidebar.style.position = 'fixed';
      sidebar.style.left = '0';
      sidebar.style.top = '0';
      sidebar.style.zIndex = '9999';
      return 'moved sidebar on-screen';
    }
    return 'no sidebar';
  }`,
});
console.log("Move sidebar:", JSON.stringify(moveAndShot));

await new Promise((r) => setTimeout(r, 200));

const ss = await tools.mcp_chrome_devtools.take_screenshot({});
console.log("=== SCREENSHOT ===");
console.log(JSON.stringify(ss).substring(0, 100));
// The important part is the image data
for (const item of (ss as any).content || []) {
  if (item.type === "image") {
    console.log("Got screenshot image, length:", item.data?.length);
  }
}

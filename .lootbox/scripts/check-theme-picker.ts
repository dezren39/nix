// Open fresh page and take screenshots of theme picker interaction
const pg = await tools.mcp_chrome_devtools.new_page({
  url: "http://localhost:8080/",
});
console.log("Page:", JSON.stringify(pg));
await new Promise((r) => setTimeout(r, 2000));

// Open sidebar (we're at 800x600 mobile)
await tools.mcp_chrome_devtools.evaluate_script({
  function: `() => { document.getElementById('sidebar-toggle').checked = true; return 'ok'; }`,
});
await new Promise((r) => setTimeout(r, 300));

// Screenshot BEFORE clicking theme button
const ss1 = await tools.mcp_chrome_devtools.take_screenshot({});
console.log("=== SS1: Before click ===");
console.log(ss1?.content?.[0]?.text);

// Click the theme picker button using CDP click tool
const snap1 = await tools.mcp_chrome_devtools.take_snapshot({});
const snapText = snap1?.content?.[0]?.text || "";
// Find the "Change theme" button uid
const match = snapText.match(/uid=([\w_]+)\s+button\s+"Change theme"/);
console.log("Button uid match:", match?.[1]);

if (match?.[1]) {
  await tools.mcp_chrome_devtools.click({ uid: match[1] });
  await new Promise((r) => setTimeout(r, 500));
}

// Screenshot AFTER clicking
const ss2 = await tools.mcp_chrome_devtools.take_screenshot({});
console.log("=== SS2: After click ===");
console.log(ss2?.content?.[0]?.text);

// Check dropdown state
const state = await tools.mcp_chrome_devtools.evaluate_script({
  function: `() => {
    const c = document.getElementById('theme-picker-container');
    const panel = c?.querySelector('.dropdown-content');
    const dropdown = c?.querySelector('.dropdown');
    return JSON.stringify({
      dropdownOpen: dropdown?.classList.contains('dropdown-open'),
      panelDisplay: panel?.style.display,
      panelComputedDisplay: panel ? getComputedStyle(panel).display : null,
      panelComputedVisibility: panel ? getComputedStyle(panel).visibility : null,
      panelComputedOpacity: panel ? getComputedStyle(panel).opacity : null,
      panelRect: panel ? panel.getBoundingClientRect() : null,
      isOpen: panel?.style.display !== 'none'
    });
  }`,
});
console.log("=== State ===");
console.log(state);

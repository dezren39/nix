// Theme picker validation at desktop viewport
// Uses correct MCP chrome_devtools API: uid-based clicks, content[] results

// Step 1: Create a new page
const page = await tools.mcp_chrome_devtools.new_page({
  url: "http://localhost:8080/",
});
console.log("Page created:", JSON.stringify(page));

// Step 2: Set viewport to desktop size via CDP
await tools.mcp_chrome_devtools.evaluate_script({
  function: `() => {
    // Can't resize from page context, but let's check current size
    return { innerWidth: window.innerWidth, innerHeight: window.innerHeight };
  }`,
});

// Wait for page to fully load
await new Promise((r) => setTimeout(r, 2500));

// Step 3: Take initial screenshot
const ss1 = await tools.mcp_chrome_devtools.take_screenshot({});
console.log("=== SCREENSHOT 1 (initial load) ===");
console.log(JSON.stringify(ss1));

// Step 4: Take a snapshot to get UIDs for elements
const snap1 = await tools.mcp_chrome_devtools.take_snapshot({});
console.log("=== SNAPSHOT 1 (initial) ===");
console.log(JSON.stringify(snap1).substring(0, 3000));

// Step 5: Check sidebar and theme button via evaluate_script
const check = await tools.mcp_chrome_devtools.evaluate_script({
  function: `() => {
    const sidebar = document.querySelector('aside');
    const sidebarStyle = sidebar ? window.getComputedStyle(sidebar) : null;
    const themeContainer = document.getElementById('theme-picker-container');
    const themeBtn = themeContainer ? themeContainer.querySelector('button') : null;
    const btnRect = themeBtn ? themeBtn.getBoundingClientRect() : null;
    return {
      sidebarExists: !!sidebar,
      sidebarDisplay: sidebarStyle?.display,
      sidebarTransform: sidebarStyle?.transform,
      sidebarWidth: sidebar?.offsetWidth,
      themeContainerExists: !!themeContainer,
      themeBtnExists: !!themeBtn,
      themeBtnText: themeBtn?.textContent?.trim(),
      themeBtnId: themeBtn?.id,
      themeBtnRect: btnRect ? { x: Math.round(btnRect.x), y: Math.round(btnRect.y), w: Math.round(btnRect.width), h: Math.round(btnRect.height) } : null,
      viewportWidth: window.innerWidth,
      viewportHeight: window.innerHeight,
      // Find if theme button has a uid/data attribute from snapshot
      themeBtnAttrs: themeBtn ? Array.from(themeBtn.attributes).map(a => a.name + '=' + a.value).join(', ') : 'N/A'
    };
  }`,
});
console.log("=== SIDEBAR & THEME CHECK ===");
console.log(JSON.stringify(check, null, 2));

// Step 6: Try to find theme button uid from snapshot text and click it
// The snapshot should contain the theme button text. Let's search for it.
const snapText = JSON.stringify(snap1);

// Look for theme-related uids in the snapshot
const themeMatches = snapText.match(
  /\[uid=([^\]]+)\][^[]*(?:theme|Theme|Dark|Light)/g,
);
console.log("=== THEME-RELATED UIDS ===");
console.log(themeMatches?.slice(0, 10));

// If viewport is mobile-sized, we need to open sidebar first
// Let's check from the evaluate_script result
const checkContent = check.content?.[0]?.text || "";
console.log("=== CHECK CONTENT ===");
console.log(checkContent);

// Try to find and click hamburger menu if in mobile mode
const hamburgerMatches = snapText.match(
  /\[uid=([^\]]+)\][^[]*(?:menu|hamburger|☰|toggle.*sidebar)/gi,
);
console.log("=== HAMBURGER UIDS ===");
console.log(hamburgerMatches?.slice(0, 5));

// Parse the check result to decide next steps
let checkData: any;
try {
  checkData = JSON.parse(checkContent);
} catch {
  checkData = null;
}

if (checkData && checkData.viewportWidth < 1024) {
  console.log(
    "MOBILE MODE DETECTED - viewport width:",
    checkData.viewportWidth,
  );
  // Need to open sidebar first - look for hamburger button
  if (hamburgerMatches && hamburgerMatches.length > 0) {
    const uidMatch = hamburgerMatches[0].match(/\[uid=([^\]]+)\]/);
    if (uidMatch) {
      console.log("Clicking hamburger:", uidMatch[1]);
      await tools.mcp_chrome_devtools.click({ uid: uidMatch[1] });
      await new Promise((r) => setTimeout(r, 500));
    }
  }
}

// Now find and click theme button
// Search for button with "Dark" or "Light" or theme picker button
const btnMatches = snapText.match(
  /\[uid=([^\]]+)\][^[]*(?:Dark|Light)\s*(?:mode)?/g,
);
console.log("=== THEME BUTTON UIDS ===");
console.log(btnMatches?.slice(0, 5));

if (btnMatches && btnMatches.length > 0) {
  const uidMatch = btnMatches[0].match(/\[uid=([^\]]+)\]/);
  if (uidMatch) {
    const themeUid = uidMatch[1];
    console.log("Clicking theme button uid:", themeUid);
    const clickResult = await tools.mcp_chrome_devtools.click({
      uid: themeUid,
    });
    console.log("Click result:", JSON.stringify(clickResult));

    await new Promise((r) => setTimeout(r, 800));

    // Step 7: Check what happened after click
    const postClick = await tools.mcp_chrome_devtools.evaluate_script({
      function: `() => {
        const container = document.getElementById('theme-picker-container');
        const allDivs = container ? container.querySelectorAll('div') : [];
        const visiblePanels = [];
        allDivs.forEach(d => {
          const s = window.getComputedStyle(d);
          const rect = d.getBoundingClientRect();
          if (s.display !== 'none' && s.visibility !== 'hidden' && d.offsetHeight > 50 && rect.width > 50) {
            visiblePanels.push({
              className: d.className?.substring(0, 80),
              h: d.offsetHeight,
              w: d.offsetWidth,
              top: Math.round(rect.top)
            });
          }
        });
        return {
          containerChildCount: container?.children?.length,
          visiblePanelCount: visiblePanels.length,
          visiblePanels: visiblePanels.slice(0, 8),
          containerHTML: container?.innerHTML?.substring(0, 600)
        };
      }`,
    });
    console.log("=== POST-CLICK STATE ===");
    console.log(JSON.stringify(postClick, null, 2));

    // Step 8: Take screenshot after clicking
    const ss2 = await tools.mcp_chrome_devtools.take_screenshot({});
    console.log("=== SCREENSHOT 2 (after theme click) ===");
    console.log(JSON.stringify(ss2));

    // Step 9: Take snapshot after clicking to see panel
    const snap2 = await tools.mcp_chrome_devtools.take_snapshot({});
    console.log("=== SNAPSHOT 2 (after theme click) ===");
    console.log(JSON.stringify(snap2).substring(0, 3000));
  }
} else {
  console.log(
    "Could not find theme button in snapshot. Trying evaluate_script click...",
  );
  // Fallback: click via JS
  await tools.mcp_chrome_devtools.evaluate_script({
    function: `() => {
      const container = document.getElementById('theme-picker-container');
      const btn = container?.querySelector('button');
      if (btn) { btn.click(); return 'clicked'; }
      return 'no button found';
    }`,
  });
  await new Promise((r) => setTimeout(r, 800));

  const ss2 = await tools.mcp_chrome_devtools.take_screenshot({});
  console.log("=== SCREENSHOT 2 (after JS click) ===");
  console.log(JSON.stringify(ss2));
}

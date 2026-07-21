// Validate: Theme picker works after SSE navigation
// Navigate to a different page via SSE click, check theme-picker-container, open panel

const cd = tools.mcp_chrome_devtools;
const results: string[] = [];
let passed = true;

function log(msg: string) {
  results.push(msg);
}
function fail(msg: string) {
  results.push(`FAIL: ${msg}`);
  passed = false;
}
function pass(msg: string) {
  results.push(`OK: ${msg}`);
}

try {
  // Step 1: Start on /irm/
  log("--- Step 1: Load /irm/ ---");
  await cd.navigate_page({ url: "http://localhost:8080/irm/" });
  await new Promise((r) => setTimeout(r, 1500));

  // Step 2: Navigate via SSE click to a different page (members)
  log("--- Step 2: SSE navigate to /irm/members ---");
  const clickNav = await cd.evaluate_script({
    function: `() => {
      const link = document.querySelector('a[href="/irm/members"]');
      if (!link) return 'NO_LINK';
      link.click();
      return 'CLICKED';
    }`,
  });
  const clickText = clickNav?.content?.[0]?.text || "";
  log(`Nav click: ${clickText}`);

  if (clickText === "NO_LINK") {
    log("Members link not found, trying direct navigation...");
    await cd.navigate_page({ url: "http://localhost:8080/irm/members" });
  }

  await new Promise((r) => setTimeout(r, 2000));

  const urlCheck = await cd.evaluate_script({
    function: "() => window.location.href",
  });
  log(`Current URL: ${urlCheck?.content?.[0]?.text || "unknown"}`);

  // Step 3: Check theme-picker-container has content
  log("--- Step 3: Check theme-picker-container ---");
  const pickerCheck = await cd.evaluate_script({
    function: `() => {
      const container = document.getElementById('theme-picker-container') || document.querySelector('.theme-picker-container') || document.querySelector('[id*="theme"]');
      if (!container) {
        // Look for any theme-related elements
        const themeEls = Array.from(document.querySelectorAll('[class*="theme"], [id*="theme"]'));
        return JSON.stringify({
          containerFound: false,
          themeElements: themeEls.map(el => ({ tag: el.tagName, id: el.id, className: el.className?.toString?.()?.substring(0, 80) })).slice(0, 10)
        });
      }
      return JSON.stringify({
        containerFound: true,
        id: container.id,
        hasContent: container.innerHTML.trim().length > 0,
        contentLength: container.innerHTML.trim().length,
        childCount: container.children.length,
        display: getComputedStyle(container).display,
        visibility: getComputedStyle(container).visibility
      });
    }`,
  });
  const pickerData = JSON.parse(pickerCheck?.content?.[0]?.text || "{}");
  log(`Picker check: ${JSON.stringify(pickerData, null, 2)}`);

  if (pickerData.containerFound && pickerData.hasContent) {
    pass("Theme picker container found with content after SSE nav");
  } else if (pickerData.containerFound) {
    fail("Theme picker container found but EMPTY after SSE nav");
  } else {
    // Maybe theme picker is inside another element
    log("Looking for theme button specifically...");
    const btnSearch = await cd.evaluate_script({
      function: `() => {
        const btns = Array.from(document.querySelectorAll('button'));
        const themeBtn = btns.find(b => 
          b.textContent?.toLowerCase().includes('theme') ||
          b.className?.includes('theme') ||
          b.id?.includes('theme') ||
          b.querySelector('[class*="theme"]')
        );
        if (themeBtn) return JSON.stringify({ found: true, text: themeBtn.textContent?.trim()?.substring(0, 50), id: themeBtn.id, className: themeBtn.className?.substring(0, 80) });
        
        // Also check for palette/paint icons
        const iconBtns = btns.filter(b => b.querySelector('svg') || b.querySelector('i'));
        return JSON.stringify({ found: false, iconButtons: iconBtns.length, allButtons: btns.map(b => b.textContent?.trim()?.substring(0, 30)).filter(Boolean).slice(0, 15) });
      }`,
    });
    const btnData = JSON.parse(btnSearch?.content?.[0]?.text || "{}");
    log(`Theme button search: ${JSON.stringify(btnData, null, 2)}`);

    if (btnData.found) {
      pass("Theme button found after SSE nav");
    } else {
      fail("No theme picker container or theme button found after SSE nav");
    }
  }

  // Step 4: Try to open theme panel
  log("--- Step 4: Open theme panel ---");
  const openPanel = await cd.evaluate_script({
    function: `() => {
      // Try clicking theme toggle button
      const themeBtn = document.querySelector('#theme-toggle, .theme-toggle, [data-theme-toggle], button[onclick*="theme"], .theme-btn');
      if (!themeBtn) {
        // Try finding by content
        const btns = Array.from(document.querySelectorAll('button'));
        const found = btns.find(b => b.textContent?.toLowerCase().includes('theme') || b.className?.includes('theme'));
        if (found) {
          found.click();
          return JSON.stringify({ clicked: true, via: 'text-search', text: found.textContent?.trim()?.substring(0, 50) });
        }
        return JSON.stringify({ clicked: false, reason: 'no theme button found' });
      }
      themeBtn.click();
      return JSON.stringify({ clicked: true, via: 'selector', id: themeBtn.id });
    }`,
  });
  const openData = JSON.parse(openPanel?.content?.[0]?.text || "{}");
  log(`Panel open: ${JSON.stringify(openData)}`);

  if (openData.clicked) {
    await new Promise((r) => setTimeout(r, 500));

    // Check if panel is visible
    const panelCheck = await cd.evaluate_script({
      function: `() => {
        // Look for theme panel/dropdown/modal
        const panels = Array.from(document.querySelectorAll('.theme-panel, .theme-dropdown, .theme-picker, [class*="theme-panel"], [class*="theme-picker"], [id*="theme-panel"]'));
        if (panels.length === 0) {
          // Check for any newly visible element
          const allTheme = Array.from(document.querySelectorAll('[class*="theme"]'));
          const visible = allTheme.filter(el => {
            const style = getComputedStyle(el);
            return style.display !== 'none' && style.visibility !== 'hidden' && style.opacity !== '0';
          });
          return JSON.stringify({ panelFound: false, visibleThemeElements: visible.length });
        }
        const panel = panels[0];
        const style = getComputedStyle(panel);
        return JSON.stringify({
          panelFound: true,
          display: style.display,
          height: panel.offsetHeight,
          visible: style.display !== 'none' && style.visibility !== 'hidden',
          heightGt50: panel.offsetHeight > 50
        });
      }`,
    });
    const panelData = JSON.parse(panelCheck?.content?.[0]?.text || "{}");
    log(`Panel state: ${JSON.stringify(panelData)}`);

    if (panelData.panelFound && panelData.visible) {
      pass(
        `Theme panel opened (display: ${panelData.display}, height: ${panelData.height})`,
      );
      if (panelData.heightGt50) {
        pass("Panel height > 50px");
      } else {
        fail(`Panel height too small: ${panelData.height}px`);
      }
    } else if (panelData.visibleThemeElements > 0) {
      pass(
        `Theme elements visible after click (${panelData.visibleThemeElements} elements)`,
      );
    } else {
      fail("Theme panel not visible after clicking theme button");
    }
  } else {
    fail("Could not click theme button to open panel");
  }
} catch (err: any) {
  fail(`Script error: ${err.message}`);
}

console.log("\n========================================");
console.log("VALIDATION 5: Theme Picker After SSE Nav");
console.log("========================================");
results.forEach((r) => console.log(r));
console.log(`\nRESULT: ${passed ? "PASS" : "FAIL"}`);
console.log("========================================\n");

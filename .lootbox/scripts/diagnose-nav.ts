// Diagnose Datastar SSE navigation and theme picker issues
// Phase 20.1.A — step 20.1.1–20.1.4

const BASE = "http://localhost:8080";

// Step 1: Navigate to home
console.log("=== Step 1: Navigate to home ===");
await tools.mcp_chrome_devtools.navigate_page({ url: BASE + "/" });

// Step 2: Check Datastar links and state
console.log("\n=== Step 2: Check Datastar links ===");
const linkCheck = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    const links = document.querySelectorAll('a[data-on-click__prevent]');
    const linkData = Array.from(links).map(a => ({
      text: a.textContent.trim(),
      href: a.getAttribute('href'),
      ds: a.getAttribute('data-on-click__prevent')
    }));
    return JSON.stringify({ count: links.length, links: linkData }, null, 2);
  }`,
});
console.log(linkCheck.content[0].text);

// Step 3: Check if Datastar module initialized
console.log("\n=== Step 3: Check Datastar initialization ===");
const dsCheck = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    // Check various Datastar indicators
    const dsScript = document.querySelector('script[src*="datastar"]');
    const dsLoaded = dsScript ? true : false;
    
    // Check if any data-* Datastar attributes have been processed
    // Datastar adds event listeners to data-on-* elements
    const hasDatastarAttrs = document.querySelectorAll('[data-on-click__prevent]').length;
    
    // Check if the module registered anything globally
    const windowKeys = Object.keys(window).filter(k => k.toLowerCase().includes('datastar') || k.toLowerCase().includes('ds'));
    
    return JSON.stringify({
      scriptTagFound: dsLoaded,
      scriptSrc: dsScript ? dsScript.src : null,
      scriptType: dsScript ? dsScript.type : null,
      elementsWithDatastarAttrs: hasDatastarAttrs,
      windowDatastarKeys: windowKeys,
    }, null, 2);
  }`,
});
console.log(dsCheck.content[0].text);

// Step 4: Check console messages
console.log("\n=== Step 4: Console messages ===");
const msgs = await tools.mcp_chrome_devtools.list_console_messages({});
console.log(msgs.content[0].text);

// Step 5: Check network requests for datastar.js
console.log("\n=== Step 5: Network requests ===");
const netReqs = await tools.mcp_chrome_devtools.list_network_requests({});
console.log(netReqs.content[0].text);

// Step 6: Try clicking a sidebar link via JS dispatch and see what happens
console.log("\n=== Step 6: Click IRM Members link via JS ===");
const clickResult = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    const link = document.querySelector('a[href="/irm/members"]');
    if (!link) return 'LINK NOT FOUND';
    link.click();
    return 'Clicked link: ' + link.textContent.trim();
  }`,
});
console.log(clickResult.content[0].text);

// Step 7: Wait a moment and check URL + content
await new Promise((r) => setTimeout(r, 3000));
const afterClick = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    return JSON.stringify({
      url: window.location.href,
      title: document.title,
      mainContentStart: document.getElementById('main-content')?.innerHTML?.substring(0, 300) || 'NO MAIN CONTENT'
    }, null, 2);
  }`,
});
console.log("\n=== Step 7: After click state ===");
console.log(afterClick.content[0].text);

// Step 8: Check console messages after click
console.log("\n=== Step 8: Console messages after click ===");
const msgs2 = await tools.mcp_chrome_devtools.list_console_messages({});
console.log(msgs2.content[0].text);

// Step 8b: Check network requests after click
console.log("\n=== Step 8b: Network requests after click ===");
const netReqs2 = await tools.mcp_chrome_devtools.list_network_requests({});
console.log(netReqs2.content[0].text);

// Step 9: Check theme picker
console.log("\n=== Step 9: Theme picker state ===");
const themePicker = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    const container = document.getElementById('theme-picker-container');
    const picker = container ? container.innerHTML.substring(0, 500) : 'NO CONTAINER';
    const themeBtn = container?.querySelector('button, [role="button"]');
    const modeToggle = document.getElementById('mode-toggle');
    return JSON.stringify({
      containerExists: !!container,
      containerChildCount: container?.children?.length || 0,
      pickerHTML: picker,
      hasButton: !!themeBtn,
      buttonText: themeBtn?.textContent?.trim() || null,
      modeToggleExists: !!modeToggle,
      currentTheme: document.documentElement.getAttribute('data-theme'),
      themeUtilAvailable: typeof window.ThemeUtil !== 'undefined',
    }, null, 2);
  }`,
});
console.log(themePicker.content[0].text);

// Step 10: Take a screenshot
console.log("\n=== Step 10: Screenshot ===");
const screenshot = await tools.mcp_chrome_devtools.take_screenshot({
  filePath:
    "/Users/drewry.pope/.config/nix/.opencode/worktrees/integration-irm/features/2026-04-07_0020.0_irm-portal-integration/screenshots/20.1-diagnosis-home.png",
});
console.log(screenshot.content[0].text);

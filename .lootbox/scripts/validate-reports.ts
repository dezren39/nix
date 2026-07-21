/**
 * validate-reports.ts — Validate reports page loads via SSE nav and Generate Report button works
 */

// Step 1: Navigate to home page
console.log("=== STEP 1: Navigate to home page ===");
await tools.mcp_chrome_devtools.navigate_page({
  url: "http://localhost:8080/",
});
await tools.mcp_chrome_devtools.wait_for({
  text: ["Portal Home"],
  timeout: 5000,
});
console.log("Home page loaded");

// Step 2: Screenshot of home page
console.log("\n=== STEP 2: Screenshot of home page ===");
const homeScreenshot = await tools.mcp_chrome_devtools.take_screenshot({});
console.log("Home screenshot taken");

// Step 3: Click the Reports link in sidebar via SSE navigation
console.log("\n=== STEP 3: Click Reports link in sidebar ===");
const clickResult = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    var links = document.querySelectorAll('a');
    var reportsLink = null;
    for (var i = 0; i < links.length; i++) {
      var text = links[i].textContent.trim();
      var href = links[i].getAttribute('href') || '';
      var dataOnClick = links[i].getAttribute('data-on-click__prevent') || links[i].getAttribute('data-on-click') || '';
      if (text.toLowerCase().includes('report') || href.includes('report') || dataOnClick.includes('report')) {
        reportsLink = links[i];
        break;
      }
    }
    if (!reportsLink) {
      // Check for buttons too
      var buttons = document.querySelectorAll('button, [role="button"]');
      for (var j = 0; j < buttons.length; j++) {
        if (buttons[j].textContent.trim().toLowerCase().includes('report')) {
          reportsLink = buttons[j];
          break;
        }
      }
    }
    if (!reportsLink) return JSON.stringify({ found: false, allLinks: Array.from(document.querySelectorAll('nav a, aside a, [class*=sidebar] a, [class*=nav] a')).map(a => ({ text: a.textContent.trim(), href: a.href, dataClick: a.getAttribute('data-on-click__prevent') || '' })) });
    reportsLink.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, view: window }));
    return JSON.stringify({ found: true, text: reportsLink.textContent.trim(), href: reportsLink.href, tag: reportsLink.tagName });
  }`,
});
console.log("Click result:", JSON.stringify(clickResult));

// Step 4: Wait for SSE navigation to complete
console.log("\n=== STEP 4: Waiting 2s for SSE nav ===");
await new Promise((r) => setTimeout(r, 2000));

// Check page state after navigation
const pageState = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    return JSON.stringify({
      url: window.location.pathname + window.location.search,
      title: document.title,
      mainContentExists: !!document.getElementById('main-content'),
      mainContentHTML: (document.getElementById('main-content') || document.querySelector('main') || document.body).innerHTML.substring(0, 500)
    });
  }`,
});
console.log("Page state after SSE nav:", JSON.stringify(pageState));

// Step 5: Screenshot of reports page
console.log("\n=== STEP 5: Screenshot of reports page ===");
const reportsScreenshot = await tools.mcp_chrome_devtools.take_screenshot({});
console.log("Reports page screenshot taken");

// Step 6: Take DOM snapshot to check Generate Report button
console.log("\n=== STEP 6: DOM snapshot — checking Generate Report button ===");
const buttonCheck = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    var buttons = document.querySelectorAll('button, [role="button"], input[type="submit"], a.btn, .btn');
    var generateBtn = null;
    var allButtons = [];
    for (var i = 0; i < buttons.length; i++) {
      var text = buttons[i].textContent.trim();
      allButtons.push({ text: text, tag: buttons[i].tagName, id: buttons[i].id, classes: buttons[i].className, onclick: buttons[i].getAttribute('onclick') || buttons[i].getAttribute('data-on-click') || buttons[i].getAttribute('data-on-click__prevent') || '' });
      if (text.toLowerCase().includes('generate')) {
        generateBtn = buttons[i];
      }
    }
    if (!generateBtn) return JSON.stringify({ found: false, allButtons: allButtons });
    var rect = generateBtn.getBoundingClientRect();
    return JSON.stringify({
      found: true,
      text: generateBtn.textContent.trim(),
      tag: generateBtn.tagName,
      id: generateBtn.id,
      visible: rect.width > 0 && rect.height > 0,
      rect: { x: rect.x, y: rect.y, w: rect.width, h: rect.height },
      onclick: generateBtn.getAttribute('onclick') || '',
      dataOnClick: generateBtn.getAttribute('data-on-click') || generateBtn.getAttribute('data-on-click__prevent') || '',
      disabled: generateBtn.disabled || false,
      allButtons: allButtons
    });
  }`,
});
console.log("Generate button check:", JSON.stringify(buttonCheck));

// Also take a snapshot for full DOM context
const snapshot = await tools.mcp_chrome_devtools.take_snapshot({});
console.log(
  "DOM snapshot taken (check for Generate Report button in output above)",
);

// Step 7: Click the Generate Report button
console.log("\n=== STEP 7: Click Generate Report button ===");

// Count existing report rows before clicking
const beforeCount = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    var rows = document.querySelectorAll('table tbody tr, .report-row, [class*=report]');
    return JSON.stringify({ rowCount: rows.length, rowTexts: Array.from(rows).slice(0, 5).map(r => r.textContent.trim().substring(0, 100)) });
  }`,
});
console.log("Before click - existing rows:", JSON.stringify(beforeCount));

const clickGenerate = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    var buttons = document.querySelectorAll('button, [role="button"], input[type="submit"], a.btn, .btn');
    for (var i = 0; i < buttons.length; i++) {
      if (buttons[i].textContent.trim().toLowerCase().includes('generate')) {
        buttons[i].dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, view: window }));
        return JSON.stringify({ clicked: true, text: buttons[i].textContent.trim() });
      }
    }
    return JSON.stringify({ clicked: false, message: 'Generate button not found' });
  }`,
});
console.log("Generate button click:", JSON.stringify(clickGenerate));

// Step 8: Wait for report generation
console.log("\n=== STEP 8: Wait 2s for report generation ===");
await new Promise((r) => setTimeout(r, 2000));

// Check results
const afterCount = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    var rows = document.querySelectorAll('table tbody tr, .report-row, [class*=report]');
    return JSON.stringify({ rowCount: rows.length, rowTexts: Array.from(rows).slice(0, 5).map(r => r.textContent.trim().substring(0, 100)) });
  }`,
});
console.log("After click - rows:", JSON.stringify(afterCount));

const afterState = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    return JSON.stringify({
      url: window.location.pathname,
      mainContent: (document.getElementById('main-content') || document.querySelector('main') || document.body).innerHTML.substring(0, 800)
    });
  }`,
});
console.log("Page state after generate:", JSON.stringify(afterState));

// Step 9: Screenshot after generate
console.log("\n=== STEP 9: Screenshot after generate ===");
const afterScreenshot = await tools.mcp_chrome_devtools.take_screenshot({});
console.log("Post-generate screenshot taken");

// Step 10: Console messages
console.log("\n=== STEP 10: Console messages ===");
const consoleMessages = await tools.mcp_chrome_devtools.list_console_messages(
  {},
);
console.log(
  "Console messages:",
  JSON.stringify(consoleMessages).substring(0, 2000),
);

console.log("\n=== VALIDATION COMPLETE ===");

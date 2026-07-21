// Test 4: Navigate to users page, check for TzUtil and data-utc behavior
await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/irm/users" });

// Check TzUtil is loaded on users page too
const tzCheck = await tools.mcp_chrome_devtools.evaluate_script({
  "function": `
    (function() {
      const tzUtil = window.TzUtil;
      if (!tzUtil) return { error: 'TzUtil not found' };
      
      // Check for data-utc elements
      const utcElements = document.querySelectorAll('[data-utc]');
      const utcData = Array.from(utcElements).map(el => ({
        tagName: el.tagName,
        dataUtc: el.getAttribute('data-utc'),
        textContent: el.textContent.trim().substring(0, 100)
      }));
      
      // Check for tz-util script tag
      const scripts = Array.from(document.querySelectorAll('script')).map(s => s.src).filter(s => s.includes('tz-util'));
      
      // Check for settings gear
      const gearBtn = document.querySelector('[onclick*="settings"], [data-modal*="settings"], a[href*="settings"]');
      
      return {
        tzUtilLoaded: true,
        currentTz: tzUtil.getTimezone(),
        dataUtcElements: utcData,
        tzUtilScripts: scripts,
        settingsGearFound: !!gearBtn,
        settingsGearInfo: gearBtn ? { tag: gearBtn.tagName, id: gearBtn.id, classes: gearBtn.className } : null
      };
    })()
  `
});
console.log("=== Users page TzUtil check ===");
console.log(JSON.stringify(tzCheck, null, 2));

// Take screenshot
const screenshot = await tools.mcp_chrome_devtools.take_screenshot({});
console.log("\n=== Users page screenshot ===");
console.log(JSON.stringify(screenshot, null, 2).substring(0, 200));

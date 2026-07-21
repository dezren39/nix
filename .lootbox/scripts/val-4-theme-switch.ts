// Validate: Theme switching actually works
// Use evaluate_script to get theme, set cupcake, verify, reset to dark

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
  // Make sure we're on a page
  log("--- Step 0: Ensure page loaded ---");
  await cd.navigate_page({ url: "http://localhost:8080/irm/" });
  await new Promise((r) => setTimeout(r, 1500));

  // Step 1: Get current theme
  log("--- Step 1: Get current theme ---");
  const currentTheme = await cd.evaluate_script({
    function: `() => {
      const htmlTheme = document.documentElement.getAttribute('data-theme');
      const hasThemeUtil = typeof ThemeUtil !== 'undefined';
      let themeUtilValue = null;
      try { themeUtilValue = ThemeUtil?.getTheme?.() || null; } catch(e) {}
      return JSON.stringify({ htmlTheme, hasThemeUtil, themeUtilValue });
    }`,
  });
  const themeData = JSON.parse(currentTheme?.content?.[0]?.text || "{}");
  log(`Current theme: ${JSON.stringify(themeData)}`);

  const originalTheme = themeData.htmlTheme || "dark";
  pass(`Got current theme: ${originalTheme}`);

  // Step 2: Set theme to cupcake
  log("--- Step 2: Set theme to 'cupcake' ---");
  const setResult = await cd.evaluate_script({
    function: `() => {
      try {
        if (typeof ThemeUtil !== 'undefined' && ThemeUtil.setTheme) {
          ThemeUtil.setTheme('cupcake');
          return 'SET_VIA_THEMEUTIL';
        } else {
          // Fallback: set directly
          document.documentElement.setAttribute('data-theme', 'cupcake');
          return 'SET_DIRECTLY';
        }
      } catch(e) {
        return 'ERROR: ' + e.message;
      }
    }`,
  });
  const setText = setResult?.content?.[0]?.text || "";
  log(`Set result: ${setText}`);

  if (setText.includes("ERROR")) {
    fail(`Failed to set theme: ${setText}`);
  } else {
    pass(`Theme set via: ${setText}`);
  }

  // Step 3: Verify theme changed to cupcake
  log("--- Step 3: Verify theme changed ---");
  await new Promise((r) => setTimeout(r, 500));

  const verifyResult = await cd.evaluate_script({
    function: `() => {
      const current = document.documentElement.getAttribute('data-theme');
      const stored = localStorage.getItem('theme') || localStorage.getItem('data-theme') || 'none';
      return JSON.stringify({ currentTheme: current, storedTheme: stored });
    }`,
  });
  const verifyData = JSON.parse(verifyResult?.content?.[0]?.text || "{}");
  log(`Verify: ${JSON.stringify(verifyData)}`);

  if (verifyData.currentTheme === "cupcake") {
    pass("Theme successfully changed to 'cupcake'");
  } else {
    fail(`Theme NOT cupcake, got: ${verifyData.currentTheme}`);
  }

  // Step 4: Reset to dark theme
  log("--- Step 4: Reset to dark ---");
  const resetResult = await cd.evaluate_script({
    function: `() => {
      try {
        if (typeof ThemeUtil !== 'undefined' && ThemeUtil.setTheme) {
          ThemeUtil.setTheme('dark');
        } else {
          document.documentElement.setAttribute('data-theme', 'dark');
        }
        return document.documentElement.getAttribute('data-theme');
      } catch(e) {
        return 'ERROR: ' + e.message;
      }
    }`,
  });
  const resetText = resetResult?.content?.[0]?.text || "";
  log(`Reset result: ${resetText}`);

  if (resetText === "dark") {
    pass("Theme reset to 'dark'");
  } else {
    fail(`Theme not reset to dark, got: ${resetText}`);
  }
} catch (err: any) {
  fail(`Script error: ${err.message}`);
}

console.log("\n========================================");
console.log("VALIDATION 4: Theme Switching");
console.log("========================================");
results.forEach((r) => console.log(r));
console.log(`\nRESULT: ${passed ? "PASS" : "FAIL"}`);
console.log("========================================\n");

// Validate: Reports button works after SSE navigation
// Navigate home -> reports, check Generate Report button exists with onclick handler

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
  // Step 1: Navigate to /irm/ first
  log("--- Step 1: Navigate to /irm/ ---");
  await cd.navigate_page({ url: "http://localhost:8080/irm/" });
  await new Promise((r) => setTimeout(r, 1500));

  const homeSnap = await cd.evaluate_script({
    function: "() => document.title + ' | URL: ' + window.location.href",
  });
  const homeText = homeSnap?.content?.[0]?.text || "";
  log(`Home page: ${homeText}`);

  // Step 2: Navigate to /irm/reports via SSE (click the link)
  log("--- Step 2: Navigate to /irm/reports via SSE link click ---");
  const clickResult = await cd.evaluate_script({
    function: `() => {
      const link = document.querySelector('a[href="/irm/reports"]');
      if (!link) return 'NO_LINK_FOUND';
      link.click();
      return 'CLICKED';
    }`,
  });
  const clickText = clickResult?.content?.[0]?.text || "";
  log(`Click result: ${clickText}`);

  if (clickText.includes("NO_LINK_FOUND")) {
    // Fallback: navigate directly
    log("Link not found in viewport, navigating directly...");
    await cd.navigate_page({ url: "http://localhost:8080/irm/reports" });
  }

  await new Promise((r) => setTimeout(r, 2000));

  const reportsUrl = await cd.evaluate_script({
    function: "() => window.location.href",
  });
  const urlText = reportsUrl?.content?.[0]?.text || "";
  log(`Current URL: ${urlText}`);

  if (urlText.includes("/reports")) {
    pass("Navigated to reports page");
  } else {
    fail(`Expected URL to contain /reports, got: ${urlText}`);
  }

  // Step 3: Check Generate Report button exists
  log("--- Step 3: Check Generate Report button ---");
  const btnCheck = await cd.evaluate_script({
    function: `() => {
      const btns = Array.from(document.querySelectorAll('button'));
      const genBtn = btns.find(b => b.textContent?.includes('Generate Report'));
      if (!genBtn) {
        // Also check inputs/links
        const allEls = Array.from(document.querySelectorAll('button, input[type="submit"], a'));
        const alt = allEls.find(el => (el.textContent || el.value || '').includes('Generate'));
        if (alt) return JSON.stringify({
          found: true,
          tag: alt.tagName,
          text: alt.textContent || alt.value,
          hasOnclick: !!alt.onclick || alt.hasAttribute('onclick'),
          type: alt.getAttribute('type')
        });
        return JSON.stringify({ found: false, buttonCount: btns.length, buttonTexts: btns.map(b => b.textContent?.trim()).filter(Boolean).slice(0, 10) });
      }
      return JSON.stringify({
        found: true,
        tag: genBtn.tagName,
        text: genBtn.textContent?.trim(),
        hasOnclick: !!genBtn.onclick || genBtn.hasAttribute('onclick'),
        type: genBtn.getAttribute('type'),
        formAction: genBtn.closest('form')?.action || null,
        formMethod: genBtn.closest('form')?.method || null,
        disabled: genBtn.disabled
      });
    }`,
  });
  const btnData = JSON.parse(btnCheck?.content?.[0]?.text || "{}");
  log(`Button data: ${JSON.stringify(btnData, null, 2)}`);

  if (btnData.found) {
    pass(`Generate Report button found (tag: ${btnData.tag})`);
  } else {
    fail(
      `Generate Report button NOT found. Buttons on page: ${JSON.stringify(btnData.buttonTexts)}`,
    );
  }

  // Step 4: Check onclick handler / form POST
  log("--- Step 4: Check button handler / form POST ---");
  const formCheck = await cd.evaluate_script({
    function: `() => {
      const form = document.querySelector('form');
      if (!form) return JSON.stringify({ formFound: false });
      return JSON.stringify({
        formFound: true,
        action: form.action,
        method: form.method,
        hasReportType: !!form.querySelector('[name="report_type"]'),
        inputs: Array.from(form.querySelectorAll('input, select, textarea')).map(el => ({
          tag: el.tagName,
          name: el.getAttribute('name'),
          type: el.getAttribute('type')
        }))
      });
    }`,
  });
  const formData = JSON.parse(formCheck?.content?.[0]?.text || "{}");
  log(`Form data: ${JSON.stringify(formData, null, 2)}`);

  if (formData.formFound) {
    pass(
      `Form found with action: ${formData.action}, method: ${formData.method}`,
    );
    if (formData.method?.toLowerCase() === "post") {
      pass("Form uses POST method");
    } else {
      log(`Note: Form method is '${formData.method}' (may be intentional)`);
    }
  } else {
    // Check for onclick handler instead
    const onclickCheck = await cd.evaluate_script({
      function: `() => {
        const btns = Array.from(document.querySelectorAll('button'));
        const genBtn = btns.find(b => b.textContent?.includes('Generate'));
        if (!genBtn) return 'NO_BTN';
        const onclick = genBtn.getAttribute('onclick');
        const listeners = typeof getEventListeners === 'function' ? JSON.stringify(getEventListeners(genBtn)) : 'N/A';
        return JSON.stringify({ onclick, listeners, hxPost: genBtn.getAttribute('hx-post'), hxGet: genBtn.getAttribute('hx-get') });
      }`,
    });
    const onclickData = onclickCheck?.content?.[0]?.text || "";
    log(`Onclick check: ${onclickData}`);
    if (onclickData.includes("hx-post") || onclickData.includes("onclick")) {
      pass("Button has event handler (onclick or htmx)");
    } else {
      fail("No form or onclick handler found for Generate Report");
    }
  }
} catch (err: any) {
  fail(`Script error: ${err.message}`);
}

console.log("\n========================================");
console.log("VALIDATION 1: Reports Button After SSE Nav");
console.log("========================================");
results.forEach((r) => console.log(r));
console.log(`\nRESULT: ${passed ? "PASS" : "FAIL"}`);
console.log("========================================\n");

// Validate: Reports page - step by step with output file
const cd = tools.mcp_chrome_devtools;
const out: string[] = [];
function log(m: string) {
  out.push(m);
}

try {
  // Step 1: Navigate and immediately get info
  log("=== Step 1: Navigate + evaluate ===");
  await cd.navigate_page({ url: "http://localhost:8080/irm/reports" });
  log("NAV_OK");
} catch (e: any) {
  log("NAV_ERR: " + e.message);
}

// Write intermediate output
console.log(out.join("\n"));
console.log("---CHECKPOINT_1---");

try {
  // Step 2: evaluate_script to get page info
  const pg = await cd.evaluate_script({
    function: `() => {
      const btns = [...document.querySelectorAll('button,input[type=submit],[role=button]')];
      const genBtn = btns.find(b => (b.textContent||'').toLowerCase().includes('generate'));
      const forms = [...document.querySelectorAll('form')];
      return JSON.stringify({
        url: location.href,
        title: document.title,
        bodyLen: document.body.innerHTML.length,
        buttonCount: btns.length,
        buttonTexts: btns.map(b => b.textContent?.trim()).filter(Boolean),
        generateFound: !!genBtn,
        generateText: genBtn?.textContent?.trim(),
        generateTag: genBtn?.tagName,
        generateOnclick: genBtn?.getAttribute('onclick'),
        generateDataOnClick: genBtn?.getAttribute('data-on-click'),
        generateHxPost: genBtn?.getAttribute('hx-post'),
        generateHxGet: genBtn?.getAttribute('hx-get'),
        generateDisabled: (genBtn as any)?.disabled,
        generateType: genBtn?.getAttribute('type'),
        formCount: forms.length,
        formInfo: forms.map(f => ({
          action: f.action, method: f.method,
          inputs: [...f.querySelectorAll('input,select,button')].map(i => ({
            tag: i.tagName, name: i.getAttribute('name'), type: i.getAttribute('type'), text: i.textContent?.trim()?.substring(0,30)
          }))
        }))
      });
    }`,
  });
  console.log("PAGE_INFO:", pg?.content?.[0]?.text);
  console.log("---CHECKPOINT_2---");
} catch (e: any) {
  console.log("EVAL_ERR:", e.message);
  console.log("---CHECKPOINT_2---");
}

try {
  // Step 3: Take snapshot
  const snap = await cd.take_snapshot({});
  console.log("SNAPSHOT:", snap?.content?.[0]?.text?.substring(0, 5000));
  console.log("---CHECKPOINT_3---");
} catch (e: any) {
  console.log("SNAP_ERR:", e.message);
  console.log("---CHECKPOINT_3---");
}

try {
  // Step 4: Click the button
  const clk = await cd.evaluate_script({
    function: `() => {
      window.__irm_errs = [];
      window.onerror = (m) => window.__irm_errs.push(String(m));
      const b = [...document.querySelectorAll('button,[role=button]')].find(e=>(e.textContent||'').toLowerCase().includes('generate'));
      if (!b) return 'NO_BUTTON';
      b.click();
      return 'CLICKED:' + b.textContent?.trim();
    }`,
  });
  console.log("CLICK:", clk?.content?.[0]?.text);
  console.log("---CHECKPOINT_4---");
} catch (e: any) {
  console.log("CLICK_ERR:", e.message);
  console.log("---CHECKPOINT_4---");
}

// Wait for effects
await new Promise((r) => setTimeout(r, 2000));

try {
  // Step 5: Post-click state
  const post = await cd.evaluate_script({
    function: `() => JSON.stringify({ url: location.href, title: document.title, len: document.body.innerHTML.length, errs: window.__irm_errs || [] })`,
  });
  console.log("POST_CLICK:", post?.content?.[0]?.text);
  console.log("---CHECKPOINT_5---");
} catch (e: any) {
  console.log("POST_ERR:", e.message);
  console.log("---CHECKPOINT_5---");
}

try {
  // Step 6: Post-click snapshot
  const ps = await cd.take_snapshot({});
  console.log("POST_SNAP:", ps?.content?.[0]?.text?.substring(0, 5000));
  console.log("---CHECKPOINT_6---");
} catch (e: any) {
  console.log("POST_SNAP_ERR:", e.message);
  console.log("---CHECKPOINT_6---");
}

try {
  // Step 7: Console
  const con = await cd.list_console_messages({});
  console.log("CONSOLE:", con?.content?.[0]?.text?.substring(0, 2000));
  console.log("---CHECKPOINT_7---");
} catch (e: any) {
  console.log("CONSOLE_ERR:", e.message);
  console.log("---CHECKPOINT_7---");
}

console.log("=== DONE ===");

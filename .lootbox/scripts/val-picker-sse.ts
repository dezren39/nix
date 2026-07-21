// val-picker-sse.ts — Diagnose theme picker behavior before/after SSE navigation
// Tests whether Datastar SSE fragment swaps break the theme picker

const cd = tools.mcp_chrome_devtools;
const SCREENSHOT_PATH =
  "/Users/drewry.pope/.config/nix/.opencode/worktrees/integration-irm/theme-picker-after-sse.png";

interface DiagResult {
  step: string;
  data: Record<string, any>;
}

const diagnostics: DiagResult[] = [];
const log = (step: string, data: any) => {
  diagnostics.push({ step, data });
  console.log(`\n--- ${step} ---`);
  console.log(typeof data === "string" ? data : JSON.stringify(data, null, 2));
};

const evalJS = async (label: string, fn: string): Promise<any> => {
  const res = await cd.evaluate_script({ function: fn });
  const text = res?.content?.[0]?.text || "";
  let parsed: any;
  try {
    parsed = JSON.parse(text);
  } catch {
    parsed = text;
  }
  log(label, parsed);
  return parsed;
};

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));

// ============================================================
// STEP 1: Full page load and verify picker works
// ============================================================
console.log("\n========================================================");
console.log("STEP 1: Full page load — verify picker on fresh load");
console.log("========================================================");

await cd.navigate_page({ url: "http://localhost:8080/" });
await wait(2000);

const beforeURL = await evalJS("1.1 Current URL", "() => window.location.href");

const beforeContainer = await evalJS(
  "1.2 Container check (before SSE)",
  `() => {
  const c = document.getElementById('theme-picker-container');
  if (!c) return { exists: false };
  return {
    exists: true,
    innerHTMLLength: c.innerHTML.length,
    childCount: c.children.length,
    display: getComputedStyle(c).display,
    visibility: getComputedStyle(c).visibility,
    outerHTMLSnippet: c.outerHTML.substring(0, 300)
  };
}`,
);

const beforeButton = await evalJS(
  "1.3 Trigger button check (before SSE)",
  `() => {
  const c = document.getElementById('theme-picker-container');
  if (!c) return { containerExists: false };
  const btn = c.querySelector('button');
  if (!btn) return { containerExists: true, buttonExists: false };
  return {
    containerExists: true,
    buttonExists: true,
    buttonId: btn.id,
    buttonText: btn.textContent?.trim()?.substring(0, 80),
    buttonClassName: btn.className?.substring(0, 120),
    buttonDisabled: btn.disabled,
    buttonAriaLabel: btn.getAttribute('aria-label'),
    hasOnClick: !!btn.onclick,
    datastarAttrs: Array.from(btn.attributes).filter(a => a.name.startsWith('data-')).map(a => a.name + '=' + a.value.substring(0, 50))
  };
}`,
);

const beforeThemeUtil = await evalJS(
  "1.4 window.ThemeUtil (before SSE)",
  `() => {
  return {
    exists: typeof window.ThemeUtil !== 'undefined',
    type: typeof window.ThemeUtil,
    keys: window.ThemeUtil ? Object.keys(window.ThemeUtil) : [],
    hasToggle: !!(window.ThemeUtil && window.ThemeUtil.toggleDropdown),
    hasInit: !!(window.ThemeUtil && window.ThemeUtil.init)
  };
}`,
);

// Open sidebar
await evalJS(
  "1.5 Open sidebar",
  `() => {
  const toggle = document.getElementById('sidebar-toggle');
  if (toggle) { toggle.checked = true; return 'sidebar opened'; }
  return 'no sidebar-toggle found';
}`,
);
await wait(300);

// Click button to open dropdown
const beforeOpen = await evalJS(
  "1.6 Open dropdown (before SSE)",
  `() => {
  const c = document.getElementById('theme-picker-container');
  const btn = c?.querySelector('button');
  if (!btn) return { clicked: false, reason: 'no button' };
  btn.click();
  return { clicked: true };
}`,
);
await wait(500);

const beforePanelState = await evalJS(
  "1.7 Panel state after click (before SSE)",
  `() => {
  const c = document.getElementById('theme-picker-container');
  if (!c) return { containerExists: false };
  
  // Look for panel/dropdown child
  const panel = c.querySelector('[style*="display"], .theme-panel, .theme-dropdown, div:not(button)');
  const allDivs = Array.from(c.querySelectorAll('div'));
  
  return {
    containerChildren: c.children.length,
    containerInnerHTMLLength: c.innerHTML.length,
    containerClassList: c.className,
    hasDropdownOpenClass: c.classList.contains('dropdown-open'),
    allDivs: allDivs.map(d => ({
      className: d.className?.substring(0, 80),
      display: getComputedStyle(d).display,
      height: d.offsetHeight,
      datastarAttrs: Array.from(d.attributes).filter(a => a.name.startsWith('data-')).map(a => a.name)
    })),
    panelDisplay: panel ? getComputedStyle(panel).display : null,
    panelStyle: panel ? panel.getAttribute('style')?.substring(0, 200) : null,
    containerHTML: c.innerHTML.substring(0, 500)
  };
}`,
);

// Close it
await evalJS(
  "1.8 Close dropdown (before SSE)",
  `() => {
  const c = document.getElementById('theme-picker-container');
  const btn = c?.querySelector('button');
  if (btn) btn.click();
  return 'closed';
}`,
);
await wait(300);

// ============================================================
// STEP 2: Trigger SSE navigation
// ============================================================
console.log("\n========================================================");
console.log("STEP 2: Trigger SSE navigation to /irm/members");
console.log("========================================================");

const sseNav = await evalJS(
  "2.1 SSE nav click",
  `() => {
  const link = document.querySelector('a[href="/irm/members"]');
  if (!link) {
    // List all nav links for debugging
    const links = Array.from(document.querySelectorAll('a[href^="/irm"]'));
    return {
      linkFound: false,
      availableLinks: links.map(l => ({
        href: l.getAttribute('href'),
        text: l.textContent?.trim()?.substring(0, 40),
        datastarAttrs: Array.from(l.attributes).filter(a => a.name.startsWith('data-')).map(a => a.name + '=' + a.value.substring(0, 60))
      }))
    };
  }
  // Record link attributes before clicking
  const attrs = Array.from(link.attributes).map(a => a.name + '=' + a.value.substring(0, 60));
  link.click();
  return { linkFound: true, clicked: true, attrs };
}`,
);

await wait(2500);

const afterURL = await evalJS(
  "2.2 URL after SSE nav",
  "() => window.location.href",
);

// ============================================================
// STEP 3: Check picker state AFTER SSE nav
// ============================================================
console.log("\n========================================================");
console.log("STEP 3: Theme picker diagnostics AFTER SSE navigation");
console.log("========================================================");

const afterContainer = await evalJS(
  "3.1 Container check (after SSE)",
  `() => {
  const c = document.getElementById('theme-picker-container');
  if (!c) return { exists: false, allThemeIds: Array.from(document.querySelectorAll('[id*="theme"]')).map(e => e.id) };
  return {
    exists: true,
    innerHTMLLength: c.innerHTML.length,
    childCount: c.children.length,
    display: getComputedStyle(c).display,
    visibility: getComputedStyle(c).visibility,
    outerHTMLSnippet: c.outerHTML.substring(0, 300)
  };
}`,
);

const afterButton = await evalJS(
  "3.2 Trigger button check (after SSE)",
  `() => {
  const c = document.getElementById('theme-picker-container');
  if (!c) return { containerExists: false };
  const btn = c.querySelector('button');
  if (!btn) return { containerExists: true, buttonExists: false, containerHTML: c.innerHTML.substring(0, 300) };
  return {
    containerExists: true,
    buttonExists: true,
    buttonId: btn.id,
    buttonText: btn.textContent?.trim()?.substring(0, 80),
    buttonClassName: btn.className?.substring(0, 120),
    buttonDisabled: btn.disabled,
    hasOnClick: !!btn.onclick,
    datastarAttrs: Array.from(btn.attributes).filter(a => a.name.startsWith('data-')).map(a => a.name + '=' + a.value.substring(0, 50))
  };
}`,
);

const afterThemeUtil = await evalJS(
  "3.3 window.ThemeUtil (after SSE)",
  `() => {
  return {
    exists: typeof window.ThemeUtil !== 'undefined',
    type: typeof window.ThemeUtil,
    keys: window.ThemeUtil ? Object.keys(window.ThemeUtil) : [],
    hasToggle: !!(window.ThemeUtil && window.ThemeUtil.toggleDropdown),
    hasInit: !!(window.ThemeUtil && window.ThemeUtil.init)
  };
}`,
);

const afterListeners = await evalJS(
  "3.4 Event listeners / onclick (after SSE)",
  `() => {
  const c = document.getElementById('theme-picker-container');
  const btn = c?.querySelector('button');
  if (!btn) return { buttonExists: false };
  return {
    onclickAttr: btn.getAttribute('onclick'),
    hasOnClickProp: !!btn.onclick,
    onclickString: btn.onclick?.toString()?.substring(0, 200),
    dataOnClick: btn.getAttribute('data-on-click') || btn.getAttribute('data-on:click'),
    allAttributes: Array.from(btn.attributes).map(a => a.name + '=' + a.value.substring(0, 80))
  };
}`,
);

// Try to open dropdown programmatically
const afterOpenAttempt = await evalJS(
  "3.5 Programmatic dropdown open (after SSE)",
  `() => {
  const c = document.getElementById('theme-picker-container');
  const btn = c?.querySelector('button');
  if (!btn) return { clicked: false, reason: 'no button' };
  
  // Record state before click
  const allDivsBefore = Array.from(c.querySelectorAll('div')).map(d => ({
    display: getComputedStyle(d).display,
    height: d.offsetHeight
  }));
  
  btn.click();
  
  // Record state after click
  const allDivsAfter = Array.from(c.querySelectorAll('div')).map(d => ({
    display: getComputedStyle(d).display,
    height: d.offsetHeight
  }));
  
  return { clicked: true, divsBefore: allDivsBefore, divsAfter: allDivsAfter };
}`,
);
await wait(500);

const afterPanelState = await evalJS(
  "3.6 Panel state after click (after SSE)",
  `() => {
  const c = document.getElementById('theme-picker-container');
  if (!c) return { containerExists: false };
  
  const allDivs = Array.from(c.querySelectorAll('div'));
  
  return {
    containerChildren: c.children.length,
    containerInnerHTMLLength: c.innerHTML.length,
    containerClassList: c.className,
    hasDropdownOpenClass: c.classList.contains('dropdown-open'),
    allDivs: allDivs.map(d => ({
      className: d.className?.substring(0, 80),
      display: getComputedStyle(d).display,
      height: d.offsetHeight,
      styleAttr: d.getAttribute('style')?.substring(0, 100),
      datastarAttrs: Array.from(d.attributes).filter(a => a.name.startsWith('data-')).map(a => a.name)
    })),
    containerHTML: c.innerHTML.substring(0, 500)
  };
}`,
);

const afterDropdownClass = await evalJS(
  "3.7 dropdown-open class check (after SSE)",
  `() => {
  const c = document.getElementById('theme-picker-container');
  if (!c) return { exists: false };
  return {
    classList: Array.from(c.classList),
    hasDropdownOpen: c.classList.contains('dropdown-open'),
    parentClassList: c.parentElement ? Array.from(c.parentElement.classList) : []
  };
}`,
);

const afterDatastarAttrs = await evalJS(
  "3.8 Datastar attributes on picker (after SSE)",
  `() => {
  const c = document.getElementById('theme-picker-container');
  if (!c) return { exists: false };
  
  const allElements = [c, ...Array.from(c.querySelectorAll('*'))];
  const datastarElements = allElements.filter(el => 
    Array.from(el.attributes).some(a => a.name.startsWith('data-'))
  );
  
  return {
    totalElements: allElements.length,
    elementsWithDataAttrs: datastarElements.length,
    details: datastarElements.map(el => ({
      tag: el.tagName,
      id: el.id,
      className: el.className?.toString()?.substring(0, 60),
      dataAttrs: Array.from(el.attributes)
        .filter(a => a.name.startsWith('data-'))
        .map(a => a.name + '=' + a.value.substring(0, 80))
    }))
  };
}`,
);

// ============================================================
// STEP 4: Take screenshot
// ============================================================
console.log("\n========================================================");
console.log("STEP 4: Screenshot with picker open after SSE nav");
console.log("========================================================");

// Ensure sidebar is open
await evalJS(
  "4.1 Open sidebar for screenshot",
  `() => {
  const toggle = document.getElementById('sidebar-toggle');
  if (toggle) { toggle.checked = true; return 'sidebar opened'; }
  return 'no sidebar-toggle found';
}`,
);
await wait(300);

// Open the picker
await evalJS(
  "4.2 Open picker for screenshot",
  `() => {
  const c = document.getElementById('theme-picker-container');
  const btn = c?.querySelector('button');
  if (btn) { btn.click(); return 'clicked'; }
  return 'no button';
}`,
);
await wait(500);

try {
  const ss = await cd.take_screenshot({ filePath: SCREENSHOT_PATH });
  const ssText = JSON.stringify(ss).substring(0, 200);
  log("4.3 Screenshot result", {
    saved: true,
    path: SCREENSHOT_PATH,
    resultSnippet: ssText,
  });
} catch (err: any) {
  log("4.3 Screenshot FAILED", { error: err.message });
}

// ============================================================
// STEP 5: Additional diagnostics
// ============================================================
console.log("\n========================================================");
console.log("STEP 5: Additional diagnostics — interference check");
console.log("========================================================");

const panelImmediate = await evalJS(
  "5.1 Panel display immediately after open",
  `() => {
  const c = document.getElementById('theme-picker-container');
  if (!c) return { exists: false };
  const allDivs = Array.from(c.querySelectorAll('div'));
  return allDivs.map(d => ({
    className: d.className?.substring(0, 60),
    display: getComputedStyle(d).display,
    visibility: getComputedStyle(d).visibility,
    opacity: getComputedStyle(d).opacity,
    pointerEvents: getComputedStyle(d).pointerEvents
  }));
}`,
);

const datastarClickHandlers = await evalJS(
  "5.2 Datastar click handlers globally",
  `() => {
  const els = document.querySelectorAll('[data-on\\\\:click], [data-on-click]');
  return {
    count: els.length,
    elements: Array.from(els).slice(0, 20).map(el => ({
      tag: el.tagName,
      id: el.id,
      className: el.className?.toString()?.substring(0, 60),
      handler: (el.getAttribute('data-on:click') || el.getAttribute('data-on-click'))?.substring(0, 100)
    }))
  };
}`,
);

const overlayCheck = await evalJS(
  "5.3 Z-index / overlay check",
  `() => {
  const sidebar = document.querySelector('aside, .sidebar, [class*="sidebar"]');
  if (!sidebar) return { sidebarFound: false };
  const sStyle = getComputedStyle(sidebar);
  
  // Check for any fixed/absolute elements that could overlay
  const overlays = Array.from(document.querySelectorAll('*')).filter(el => {
    const s = getComputedStyle(el);
    return (s.position === 'fixed' || s.position === 'absolute') && parseInt(s.zIndex) > 10;
  });
  
  return {
    sidebarFound: true,
    sidebarTag: sidebar.tagName,
    sidebarDisplay: sStyle.display,
    sidebarOverflow: sStyle.overflow,
    sidebarOverflowY: sStyle.overflowY,
    sidebarOverflowX: sStyle.overflowX,
    sidebarZIndex: sStyle.zIndex,
    sidebarPosition: sStyle.position,
    potentialOverlays: overlays.slice(0, 10).map(el => ({
      tag: el.tagName,
      id: el.id,
      className: el.className?.toString()?.substring(0, 60),
      zIndex: getComputedStyle(el).zIndex,
      position: getComputedStyle(el).position
    }))
  };
}`,
);

const documentClickCheck = await evalJS(
  "5.4 Document click listener interference",
  `() => {
  // Check if clicking the document closes the dropdown
  // We'll check the picker state, dispatch a click on document, then check again
  const c = document.getElementById('theme-picker-container');
  if (!c) return { exists: false };
  
  const allDivsBefore = Array.from(c.querySelectorAll('div')).map(d => ({
    display: getComputedStyle(d).display,
    height: d.offsetHeight
  }));
  
  // Dispatch document click
  document.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  
  const allDivsAfter = Array.from(c.querySelectorAll('div')).map(d => ({
    display: getComputedStyle(d).display,
    height: d.offsetHeight
  }));
  
  return {
    divsBefore: allDivsBefore,
    divsAfter: allDivsAfter,
    changed: JSON.stringify(allDivsBefore) !== JSON.stringify(allDivsAfter)
  };
}`,
);

const consoleErrors = await cd.list_console_messages({});
const errorMessages = (consoleErrors?.content?.[0]?.text || "")
  .split("\n")
  .filter((line: string) => /error|warn|uncaught|exception/i.test(line))
  .slice(0, 20);
log("5.5 Console errors/warnings", errorMessages);

// ============================================================
// SUMMARY
// ============================================================
console.log(
  "\n\n================================================================",
);
console.log("SUMMARY: Theme Picker Before/After SSE Navigation");
console.log("================================================================");

const beforeWorked =
  beforeContainer?.exists &&
  beforeContainer?.innerHTMLLength > 0 &&
  beforeButton?.buttonExists;
const afterExists =
  afterContainer?.exists &&
  afterContainer?.innerHTMLLength > 0 &&
  afterButton?.buttonExists;

// Check if panel actually opened after SSE
const panelOpenedAfterSSE = afterPanelState?.allDivs?.some(
  (d: any) =>
    d.display === "flex" ||
    d.display === "grid" ||
    (d.display === "block" && d.height > 50),
);

console.log(`\n1. BEFORE SSE nav:`);
console.log(`   Container exists:    ${beforeContainer?.exists}`);
console.log(`   innerHTML length:    ${beforeContainer?.innerHTMLLength}`);
console.log(`   Button exists:       ${beforeButton?.buttonExists}`);
console.log(`   ThemeUtil exists:    ${beforeThemeUtil?.exists}`);
console.log(
  `   Panel opened:        ${beforePanelState?.allDivs?.length > 0 ? "YES (divs found)" : "NO"}`,
);
console.log(`   VERDICT:             ${beforeWorked ? "WORKING" : "BROKEN"}`);

console.log(`\n2. AFTER SSE nav:`);
console.log(`   Container exists:    ${afterContainer?.exists}`);
console.log(`   innerHTML length:    ${afterContainer?.innerHTMLLength}`);
console.log(`   Button exists:       ${afterButton?.buttonExists}`);
console.log(`   ThemeUtil exists:    ${afterThemeUtil?.exists}`);
console.log(
  `   Panel opened:        ${panelOpenedAfterSSE ? "YES" : "NO / UNKNOWN"}`,
);
console.log(`   dropdown-open class: ${afterDropdownClass?.hasDropdownOpen}`);
console.log(
  `   VERDICT:             ${afterExists && panelOpenedAfterSSE ? "WORKING" : afterExists ? "EXISTS BUT MAY NOT OPEN" : "BROKEN"}`,
);

console.log(`\n3. CHANGES:`);
console.log(`   innerHTML before:    ${beforeContainer?.innerHTMLLength}`);
console.log(`   innerHTML after:     ${afterContainer?.innerHTMLLength}`);
console.log(
  `   Same length:         ${beforeContainer?.innerHTMLLength === afterContainer?.innerHTMLLength}`,
);
console.log(`   Button before:       ${beforeButton?.buttonExists}`);
console.log(`   Button after:        ${afterButton?.buttonExists}`);
console.log(
  `   ThemeUtil before:    ${beforeThemeUtil?.exists} (keys: ${beforeThemeUtil?.keys?.join(", ")})`,
);
console.log(
  `   ThemeUtil after:     ${afterThemeUtil?.exists} (keys: ${afterThemeUtil?.keys?.join(", ")})`,
);

console.log(`\n4. Screenshot: ${SCREENSHOT_PATH}`);

console.log(`\n5. Datastar interference:`);
console.log(
  `   Datastar click handlers:     ${datastarClickHandlers?.count || 0}`,
);
console.log(
  `   Elements w/ data- attrs:     ${afterDatastarAttrs?.elementsWithDataAttrs || 0}`,
);
console.log(
  `   Sidebar overflow:            ${overlayCheck?.sidebarOverflow} / ${overlayCheck?.sidebarOverflowY}`,
);
console.log(`   Sidebar z-index:             ${overlayCheck?.sidebarZIndex}`);
console.log(
  `   Potential overlays:           ${overlayCheck?.potentialOverlays?.length || 0}`,
);
console.log(`   Doc click closed dropdown:    ${documentClickCheck?.changed}`);
console.log(`   Console errors:               ${errorMessages.length}`);

console.log(
  "\n================================================================",
);

/**
 * Validates theme picker dropdown overflow fix via Chrome DevTools MCP.
 *
 * Uses lootbox's mcp_chrome_devtools proxy — no raw CDP needed.
 *
 * Prerequisites:
 *   - Chrome open (the MCP server handles the connection)
 *   - The OP app running at http://localhost:8080
 *
 * @example lootbox validate-theme-dropdown.ts
 */

const APP_URL = "http://localhost:8080";
const cd = tools.mcp_chrome_devtools;

// --- helpers ---
interface ValidationResult {
  name: string;
  passed: boolean;
  detail: string;
}

/** Extract the text content from an MCP tool result */
function text(r: { content: Array<{ type: string; text?: string }> }): string {
  return r.content.map((c) => c.text ?? "").join("");
}

/** Run a JS function in the page and return its JSON-parsed value */
async function evalJs(fn: string): Promise<unknown> {
  const r = await cd.evaluate_script({ function: fn });
  const raw = text(r);
  // MCP returns "Script ran on page and returned:\n```json\n{...}\n```"
  const fenceMatch = raw.match(/```(?:json)?\n([\s\S]*?)\n```/);
  const payload = fenceMatch ? fenceMatch[1] : raw;
  try {
    return JSON.parse(payload);
  } catch {
    return payload;
  }
}

// --- validations ---

async function validateDropdownOutsideNav(): Promise<ValidationResult> {
  const isOutside = await evalJs(`() => {
    const dropdown = document.querySelector('.dropdown');
    if (!dropdown) return null;
    let el = dropdown.parentElement;
    while (el) {
      if (el.tagName === 'NAV') return false;
      el = el.parentElement;
    }
    return true;
  }`);

  if (isOutside === null) {
    return {
      name: "dropdown-outside-nav",
      passed: false,
      detail: "Dropdown element not found",
    };
  }
  return {
    name: "dropdown-outside-nav",
    passed: isOutside === true,
    detail: isOutside
      ? "Dropdown is outside <nav>"
      : "FAIL: Dropdown is inside <nav> — will be clipped by overflow",
  };
}

async function validateNavBarWrapperExists(): Promise<ValidationResult> {
  const result = (await evalJs(`() => {
    const wrapper = document.querySelector('.nav-bar');
    if (!wrapper) return { exists: false };
    return {
      exists: true,
      hasNav: wrapper.querySelector('nav') !== null,
      hasDropdown: wrapper.querySelector('.dropdown') !== null,
    };
  }`)) as { exists: boolean; hasNav?: boolean; hasDropdown?: boolean } | null;

  if (!result?.exists) {
    return {
      name: "nav-bar-wrapper",
      passed: false,
      detail: ".nav-bar wrapper not found",
    };
  }
  const ok = result.hasNav === true && result.hasDropdown === true;
  return {
    name: "nav-bar-wrapper",
    passed: ok,
    detail: ok
      ? ".nav-bar wrapper contains both <nav> and .dropdown"
      : `FAIL: wrapper has nav=${result.hasNav}, dropdown=${result.hasDropdown}`,
  };
}

async function validateNavOverflow(): Promise<ValidationResult> {
  const overflow = (await evalJs(
    `() => getComputedStyle(document.querySelector('nav')).overflowX`,
  )) as string | null;

  return {
    name: "nav-overflow-x",
    passed: overflow === "auto",
    detail:
      overflow === "auto"
        ? "Nav has overflow-x:auto for tab scrolling"
        : `FAIL: Nav overflow-x is '${overflow}', expected 'auto'`,
  };
}

async function validateDropdownParentNoClip(): Promise<ValidationResult> {
  const result = (await evalJs(`() => {
    const dropdown = document.querySelector('.dropdown');
    if (!dropdown) return null;
    const parent = dropdown.parentElement;
    const style = getComputedStyle(parent);
    return {
      overflowX: style.overflowX,
      overflowY: style.overflowY,
      tagName: parent.tagName,
      className: parent.className.slice(0, 80),
    };
  }`)) as {
    overflowX: string;
    overflowY: string;
    tagName: string;
    className: string;
  } | null;

  if (!result) {
    return {
      name: "dropdown-parent-no-clip",
      passed: false,
      detail: "Dropdown not found",
    };
  }

  const clipped = ["auto", "scroll", "hidden"];
  const xClips = clipped.includes(result.overflowX);
  const yClips = clipped.includes(result.overflowY);
  const ok = !xClips && !yClips;

  return {
    name: "dropdown-parent-no-clip",
    passed: ok,
    detail: ok
      ? `Dropdown parent <${result.tagName}> has visible overflow`
      : `FAIL: Dropdown parent <${result.tagName}> overflow-x:${result.overflowX}, overflow-y:${result.overflowY}`,
  };
}

async function validateDropdownRendersBelow(): Promise<ValidationResult> {
  // Use MCP click (real browser click) to open the focus-based dropdown
  // First, get the snapshot to find the theme label uid
  const snap = await cd.take_snapshot({});
  const snapText = text(snap);
  // Find the theme label uid (e.g. uid=1_19 LabelText)
  const labelMatch = snapText.match(/uid=(\S+)\s+LabelText/);
  if (!labelMatch) {
    return {
      name: "dropdown-renders-below",
      passed: false,
      detail: "Could not find theme LabelText in accessibility snapshot",
    };
  }

  // Click via MCP (real focus event)
  await cd.click({ uid: labelMatch[1] });
  await new Promise((r) => setTimeout(r, 400));

  // Now measure positions with the dropdown open
  const positions = (await evalJs(`() => {
    const navBar = document.querySelector('.nav-bar');
    const ddContent = document.querySelector('.dropdown-content');
    if (!navBar || !ddContent) return null;
    const navRect = navBar.getBoundingClientRect();
    const ddRect = ddContent.getBoundingClientRect();
    return {
      navBarBottom: Math.round(navRect.bottom),
      dropdownBottom: Math.round(ddRect.bottom),
      dropdownHeight: Math.round(ddRect.height),
    };
  }`)) as {
    navBarBottom: number;
    dropdownBottom: number;
    dropdownHeight: number;
  } | null;

  // Close dropdown by clicking main
  await cd.click({ uid: "1_21" });

  if (!positions) {
    return {
      name: "dropdown-renders-below",
      passed: false,
      detail: "Could not find .nav-bar or .dropdown-content",
    };
  }

  const ok =
    positions.dropdownHeight > 0 &&
    positions.dropdownBottom > positions.navBarBottom;
  return {
    name: "dropdown-renders-below",
    passed: ok,
    detail: ok
      ? `Dropdown extends ${positions.dropdownBottom - positions.navBarBottom}px below nav bar (height=${positions.dropdownHeight}px)`
      : `FAIL: dropdown bottom=${positions.dropdownBottom}, nav bottom=${positions.navBarBottom}, height=${positions.dropdownHeight}`,
  };
}

async function validateNoConsoleErrors(): Promise<ValidationResult> {
  const msgs = await cd.list_console_messages({ types: ["error"] });
  const raw = text(msgs);
  const hasErrors =
    raw.includes("error") && !raw.includes("No console messages");

  return {
    name: "no-console-errors",
    passed: !hasErrors,
    detail: hasErrors
      ? `FAIL: Console errors detected: ${raw.slice(0, 200)}`
      : "No JS console errors on page",
  };
}

// --- main ---
async function main(): Promise<string> {
  const lines: string[] = [];
  const log = (msg: string) => lines.push(msg);

  log("=== Theme Picker Dropdown Overflow Validation ===");
  log(`App URL: ${APP_URL}`);
  log("Transport: lootbox mcp_chrome_devtools proxy\n");

  // Navigate to the app
  await cd.navigate_page({ url: APP_URL });
  await cd.wait_for({ text: ["Theme"], timeout: 5000 });

  log("Page loaded. Running validations...\n");

  const results: ValidationResult[] = [];
  results.push(await validateDropdownOutsideNav());
  results.push(await validateNavBarWrapperExists());
  results.push(await validateNavOverflow());
  results.push(await validateDropdownParentNoClip());
  results.push(await validateDropdownRendersBelow());
  results.push(await validateNoConsoleErrors());

  log("--- Results ---\n");
  let allPassed = true;
  for (const r of results) {
    const icon = r.passed ? "PASS" : "FAIL";
    log(`[${icon}] ${r.name}: ${r.detail}`);
    if (!r.passed) allPassed = false;
  }

  log(
    `\n--- Summary: ${results.filter((r) => r.passed).length}/${results.length} passed ---`,
  );

  if (allPassed) {
    log("\nAll validations PASSED — dropdown renders correctly.");
  } else {
    log("\nValidation FAILED — dropdown overflow issue detected.");
  }

  return lines.join("\n");
}

const output = await main();
console.log(output);

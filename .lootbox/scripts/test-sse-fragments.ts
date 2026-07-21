// @ts-nocheck
// Phase 19.7.E.6 — Verify SSE fragment responses
const cd = tools.mcp_chrome_devtools;
const SHOT_DIR =
  "/Users/drewry.pope/.config/nix/.opencode/worktrees/theme-align/features/2026-04-06_0019.0_theme-alignment-bootstrap-removal/screenshots";

function text(r: any): string {
  return r.content.map((c: any) => c.text ?? "").join("");
}

async function evalJs(fn: string): Promise<any> {
  const r = await cd.evaluate_script({ function: fn });
  const raw = text(r);
  const m = raw.match(/```(?:json)?\n([\s\S]*?)\n```/);
  try {
    return JSON.parse(m ? m[1] : raw);
  } catch {
    return m ? m[1] : raw;
  }
}

async function shot(name: string): Promise<void> {
  await cd.take_screenshot({ filePath: `${SHOT_DIR}/${name}` });
  shotCount++;
  console.log(`  [SCREENSHOT] ${name}`);
}

async function wait(ms: number): Promise<void> {
  await evalJs(
    `async () => { await new Promise(r => setTimeout(r, ${ms})); return "waited ${ms}ms"; }`,
  );
}

let shotCount = 0;
let pass = 0;
let fail = 0;
const results: string[] = [];

function check(label: string, condition: boolean, detail?: string): void {
  const status = condition ? "PASS" : "FAIL";
  if (condition) pass++;
  else fail++;
  const msg = `  [${status}] ${label}${detail ? ` (${detail})` : ""}`;
  console.log(msg);
  results.push(msg);
}

// --- Begin test ---
console.log("=== Phase 19.7.E.6: SSE Fragment Response Test ===\n");

// Step 1-2: Navigate and screenshot
console.log("Step 1-2: Navigate to http://localhost:8080/");
await cd.navigate_page({ url: "http://localhost:8080/" });
await wait(1500);
await shot("E06-sse-01-initial.png");

// Step 3-7: Test SSE fragment responses for each route
const routes = [
  "/irm/incidents",
  "/irm/teams",
  "/irm/members",
  "/vod",
  "/readiness",
];

console.log("\nStep 3-7: Test SSE fragment responses for routes");
for (const route of routes) {
  console.log(`\n--- Testing SSE: ${route}?datastar={} ---`);

  const sseResult = await evalJs(`async () => {
    try {
      const resp = await fetch('${route}?datastar={}', {
        headers: { 'Accept': 'text/event-stream' }
      });
      const ct = resp.headers.get('content-type');
      const body = await resp.text();
      return {
        status: resp.status,
        contentType: ct,
        bodyLength: body.length,
        bodyStart: body.substring(0, 500),
        hasPatchElements: body.includes('datastar-patch-elements'),
        hasSelector: body.includes('selector #main-content')
      };
    } catch (e) {
      return { error: e.message };
    }
  }`);

  if (sseResult?.error) {
    check(`${route} SSE fetch`, false, `error: ${sseResult.error}`);
    continue;
  }

  console.log(
    `  status: ${sseResult.status}, contentType: ${sseResult.contentType}, bodyLength: ${sseResult.bodyLength}`,
  );
  console.log(`  bodyStart: ${sseResult.bodyStart?.substring(0, 200)}...`);

  check(
    `${route} status === 200`,
    sseResult.status === 200,
    `got ${sseResult.status}`,
  );
  check(
    `${route} contentType contains 'text/event-stream'`,
    sseResult.contentType?.includes("text/event-stream"),
    `got "${sseResult.contentType}"`,
  );
  check(
    `${route} body contains 'datastar-patch-elements'`,
    sseResult.hasPatchElements === true,
    `hasPatchElements=${sseResult.hasPatchElements}`,
  );
  check(
    `${route} body contains 'selector #main-content'`,
    sseResult.hasSelector === true,
    `hasSelector=${sseResult.hasSelector}`,
  );
}

// Step 8: Screenshot after fetch tests
console.log("\nStep 8: Screenshot after SSE fetch tests");
await shot("E06-sse-02-fetch-results.png");

// Step 9-10: Test normal (non-SSE) request returns full HTML
console.log("\nStep 9-10: Test normal request returns full HTML");
const routes2 = ["/irm/incidents", "/irm/teams", "/irm/members"];

for (const route of routes2) {
  console.log(`\n--- Testing normal HTML: ${route} ---`);

  const htmlResult = await evalJs(`async () => {
    try {
      const resp = await fetch('${route}');
      const ct = resp.headers.get('content-type');
      const body = await resp.text();
      return {
        status: resp.status,
        contentType: ct,
        hasDoctype: body.trimStart().startsWith('<!'),
        bodyLength: body.length
      };
    } catch (e) {
      return { error: e.message };
    }
  }`);

  if (htmlResult?.error) {
    check(`${route} normal fetch`, false, `error: ${htmlResult.error}`);
    continue;
  }

  console.log(
    `  status: ${htmlResult.status}, contentType: ${htmlResult.contentType}, hasDoctype: ${htmlResult.hasDoctype}, bodyLength: ${htmlResult.bodyLength}`,
  );

  check(
    `${route} normal contentType contains 'text/html'`,
    htmlResult.contentType?.includes("text/html"),
    `got "${htmlResult.contentType}"`,
  );
  check(
    `${route} normal response has doctype`,
    htmlResult.hasDoctype === true,
    `hasDoctype=${htmlResult.hasDoctype}`,
  );
}

// Summary
console.log("\n=== SUMMARY ===");
console.log(`Screenshots taken: ${shotCount}`);
console.log(`Pass: ${pass}  Fail: ${fail}  Total: ${pass + fail}`);
console.log(
  fail === 0 ? "\n*** ALL TESTS PASSED ***" : "\n*** SOME TESTS FAILED ***",
);
results.forEach((r) => console.log(r));

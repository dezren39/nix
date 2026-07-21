// Navigate to latest rev2, expand CE + first team, take screenshot
await tools.mcp_chrome_devtools.navigate_page({
  url: "file:///Users/drewry.pope/git/operations-portal/ops/scripts/logs/2026-04-14T17-00-50Z_20R8_page_report_rev2.html",
});
await new Promise((r) => setTimeout(r, 500));

// Expand CE
await tools.mcp_chrome_devtools.evaluate_script({
  function:
    'document.querySelector(".row-vs[data-vs=\\"CE\\"]").click()',
});
await new Promise((r) => setTimeout(r, 300));

// Expand first team in CE
await tools.mcp_chrome_devtools.evaluate_script({
  function:
    'document.querySelector(".row-team[data-vs=\\"CE\\"]").click()',
});
await new Promise((r) => setTimeout(r, 300));

// Scroll to CE area
await tools.mcp_chrome_devtools.evaluate_script({
  function:
    'document.querySelector(".row-vs[data-vs=\\"CE\\"]").scrollIntoView({block:"start"})',
});
await new Promise((r) => setTimeout(r, 300));

// Take screenshot
const r = await tools.mcp_chrome_devtools.take_screenshot({});
console.log("Screenshot taken");

// Also get snapshot of the CE area
const snap = await tools.mcp_chrome_devtools.take_snapshot({});
const text: string = snap.content[0]?.text ?? "";
const ceIdx = text.indexOf("CE\"\n");
if (ceIdx > 0) console.log(text.slice(ceIdx, ceIdx + 2000));
else console.log("showing last 2000:", text.slice(-2000));

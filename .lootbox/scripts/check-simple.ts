// Check grid state using simpler evaluate_script calls
await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/irm/users" });
await new Promise(r => setTimeout(r, 3000));

// Simple string eval
const r1 = await tools.mcp_chrome_devtools.evaluate_script({
  function: "document.title"
});
console.log("Page title:", r1?.content?.[0]?.text || JSON.stringify(r1));

const r2 = await tools.mcp_chrome_devtools.evaluate_script({
  function: "typeof window.__currentGridApi"
});
console.log("gridApi type:", r2?.content?.[0]?.text || JSON.stringify(r2));

const r3 = await tools.mcp_chrome_devtools.evaluate_script({
  function: "typeof window.createSearchFilter"
});
console.log("createSearchFilter type:", r3?.content?.[0]?.text || JSON.stringify(r3));

const r4 = await tools.mcp_chrome_devtools.evaluate_script({
  function: "typeof window.onUsersSearchChanged"
});
console.log("onUsersSearchChanged type:", r4?.content?.[0]?.text || JSON.stringify(r4));

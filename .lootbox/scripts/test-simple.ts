console.log("Step 1: start");
await tools.mcp_chrome_devtools.navigate_page({
  url: "http://localhost:8080/",
});
console.log("Step 2: navigated");
await new Promise((r) => setTimeout(r, 1000));
console.log("Step 3: waited");

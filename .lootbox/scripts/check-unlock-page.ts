await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/support-actions/unlock-user" });
const screenshot = await tools.mcp_chrome_devtools.take_screenshot({});
console.log(screenshot);

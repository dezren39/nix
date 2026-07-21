await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/users" });
// Wait for page to load
await new Promise(r => setTimeout(r, 3000));
const msgs = await tools.mcp_chrome_devtools.list_console_messages({});
console.log("CONSOLE MESSAGES:", JSON.stringify(msgs, null, 2));
const snap = await tools.mcp_chrome_devtools.take_snapshot({});
console.log("SNAPSHOT:", snap);

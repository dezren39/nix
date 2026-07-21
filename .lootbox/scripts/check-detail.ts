await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/bulk-pages/DUGOBQ" });
await new Promise(r => setTimeout(r, 3000));
const msgs = await tools.mcp_chrome_devtools.list_console_messages({});
console.log(JSON.stringify(msgs, null, 2));

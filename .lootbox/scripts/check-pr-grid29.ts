const r = await tools.mcp_chrome_devtools.take_screenshot({});
console.log(r.content[0].text.substring(0, 100));

const snap = await tools.mcp_chrome_devtools.take_snapshot({});
const text = snap.content[0].text;
console.log(text);

const B = "http://127.0.0.1:8137";
const cd = tools.mcp_chrome_devtools;
const out = (x: unknown) => console.log(JSON.stringify(x).slice(0, 4000));

await cd.navigate_page({ pageId: 1, url: `${B}/admin/storage-status` });
await new Promise((r) => setTimeout(r, 1500));
out(await cd.take_snapshot({ pageId: 1 }));

// First list pages to find or create one
const pages = await tools.mcp_chrome_devtools.list_pages({});
console.log("=== PAGES ===");
console.log(JSON.stringify(pages, null, 2));

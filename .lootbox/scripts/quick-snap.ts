// Simple navigation + snapshot of portal home
console.log("Starting snapshot...");
try {
  const nav = await tools.mcp_chrome_devtools.navigate_page({
    url: "http://localhost:8080/",
  });
  console.log("Navigation result:", JSON.stringify(nav).substring(0, 1000));
} catch (e) {
  console.error("Error:", e);
}

const r = await tools.mcp_chrome_devtools.evaluate_script({
  function: "function() { return document.title; }",
});
console.log("TITLE:", JSON.stringify(r));

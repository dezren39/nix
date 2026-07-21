export {};
// Test nav click + URL update + theme picker
// Navigate, click a sidebar link, check if URL updated

await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080" });
await new Promise((r) => setTimeout(r, 2000));

// Check initial state
const urlBefore = await tools.mcp_chrome_devtools.evaluate_script({
  expression: "window.location.href",
});
console.log("URL before:", urlBefore.content[0].text);

// Click the Reports link in sidebar (under IRM section)
// First open the IRM details if needed
try {
  await tools.mcp_chrome_devtools.click({
    selector: "details:has(a[href='/irm/reports']) summary",
  });
  await new Promise((r) => setTimeout(r, 500));
} catch (e) {
  console.log("IRM details may already be open");
}

await tools.mcp_chrome_devtools.click({ selector: "a[href='/irm/reports']" });
await new Promise((r) => setTimeout(r, 2000));

const urlAfter = await tools.mcp_chrome_devtools.evaluate_script({
  expression: "window.location.href",
});
const titleAfter = await tools.mcp_chrome_devtools.evaluate_script({
  expression: "document.title",
});
console.log("URL after click:", urlAfter.content[0].text);
console.log("Title after click:", titleAfter.content[0].text);

// Check main content
const mainContent = await tools.mcp_chrome_devtools.evaluate_script({
  expression:
    "document.getElementById('main-content')?.textContent?.substring(0, 200)",
});
console.log("Main content:", mainContent.content[0].text);

// Test theme picker - click on it
console.log("\n--- Theme Picker Test ---");
const pickerExists = await tools.mcp_chrome_devtools.evaluate_script({
  expression:
    "!!document.getElementById('theme-picker-container') && document.getElementById('theme-picker-container').innerHTML.substring(0, 100)",
});
console.log("Picker container:", pickerExists.content[0].text);

// Try clicking the theme picker button
try {
  await tools.mcp_chrome_devtools.click({
    selector: "#theme-picker-container button",
  });
  await new Promise((r) => setTimeout(r, 500));

  const dropdownOpen = await tools.mcp_chrome_devtools.evaluate_script({
    expression:
      "document.querySelector('#theme-picker-container .dropdown')?.classList.contains('dropdown-open')",
  });
  console.log("Dropdown opened:", dropdownOpen.content[0].text);

  const panelVisible = await tools.mcp_chrome_devtools.evaluate_script({
    expression:
      "document.querySelector('#theme-picker-container .dropdown-content')?.style.display",
  });
  console.log("Panel display:", panelVisible.content[0].text);
} catch (e) {
  console.log("Theme picker click error:", e);
}

// Take screenshot
const r = await tools.mcp_chrome_devtools.take_screenshot({});
const img = r.content.find((c: any) => c.type === "image");
if (img) {
  console.log("\nScreenshot base64 length:", (img.data as string).length);
}

// Check console messages for errors
const msgs = await tools.mcp_chrome_devtools.list_console_messages({});
console.log("\nConsole:", msgs.content[0].text);

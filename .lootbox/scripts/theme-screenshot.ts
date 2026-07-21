// Take screenshot of theme picker open, save to file
// Move sidebar on-screen first since viewport is 800x600

// Step 1: Open the theme picker
await tools.mcp_chrome_devtools.evaluate_script({
  function: `() => {
    // Move sidebar on-screen
    const sidebar = document.querySelector('aside');
    if (sidebar) {
      sidebar.style.transform = 'translateX(0)';
      sidebar.style.position = 'fixed';
      sidebar.style.left = '0';
      sidebar.style.top = '0';
      sidebar.style.zIndex = '9999';
      sidebar.style.height = '100vh';
    }
    // Open the theme picker
    const container = document.getElementById('theme-picker-container');
    const btn = container?.querySelector('button');
    if (btn) btn.click();
    return 'done';
  }`,
});

await new Promise((r) => setTimeout(r, 800));

// Step 2: Take screenshot
const ss = await tools.mcp_chrome_devtools.take_screenshot({});

// Step 3: Save to file
const imgContent = (ss as any).content?.find((c: any) => c.type === "image");
if (imgContent?.data) {
  const bytes = Uint8Array.from(atob(imgContent.data), (c) => c.charCodeAt(0));
  await Deno.writeFile(
    "/Users/drewry.pope/.config/nix/.opencode/worktrees/integration-irm/theme-picker-screenshot.png",
    bytes,
  );
  console.log(
    "Saved to: /Users/drewry.pope/.config/nix/.opencode/worktrees/integration-irm/theme-picker-screenshot.png",
  );
} else {
  console.log("No image data found");
  console.log(JSON.stringify(ss).substring(0, 500));
}

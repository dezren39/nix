await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/support-actions/edit-vod-customer-config?_t=" + Date.now() });
await new Promise(r => setTimeout(r, 4000));

// Check JsDiff loaded and mode toggle exists
const r = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { return JSON.stringify({ hasJsDiff: !!globalThis.JsDiff, hasStructuredPatch: !!(globalThis.JsDiff && globalThis.JsDiff.structuredPatch), modeToggle: !!document.getElementById('config-edit-diff-mode-toggle'), modeToggleHidden: document.getElementById('config-edit-diff-mode-toggle').classList.contains('hidden') }); }" });
console.log("Setup:", r.content[0].text);

// Test structuredPatch
const r2 = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var old = 'line1\\nline2\\nline3\\nline4\\n'; var nw = 'line1\\nline2-changed\\nline3\\nline4\\n'; var patch = globalThis.JsDiff.structuredPatch('a', 'b', old, nw, '', '', {context:3}); return JSON.stringify({ hunkCount: patch.hunks.length, firstHunkLines: patch.hunks[0] ? patch.hunks[0].lines.length : 0 }); }" });
console.log("Patch:", r2.content[0].text);

const msgs = await tools.mcp_chrome_devtools.list_console_messages({});
const text = JSON.stringify(msgs);
const errIdx = text.indexOf('[error]');
console.log("Errors:", errIdx > -1 ? text.substring(errIdx, errIdx + 200) : "none");

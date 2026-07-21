await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/support-actions/edit-vod-customer-config?_t=" + Date.now() });
await new Promise(r => setTimeout(r, 4000));

// Check YAML lib is loaded
const r1 = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { return JSON.stringify({ hasYAML: !!globalThis.YAML, hasParseDoc: !!(globalThis.YAML && globalThis.YAML.parseDocument), hasConfigForm: !!globalThis.__configForm }); }" });
console.log("Libs:", r1.content[0].text);

// Test roundtrip: parse a YAML with comments, set a value, check comments preserved
const r2 = await tools.mcp_chrome_devtools.evaluate_script({ function: "function() { var yaml = '# Customer config\\ncustomer_number: 123 # account id\\ncustomer_long_name: Test Corp\\n# end\\n'; var doc = YAML.parseDocument(yaml); doc.set('customer_number', 456); var result = doc.toString(); return JSON.stringify({ input: yaml, output: result, commentsPreserved: result.indexOf('# Customer config') > -1 && result.indexOf('# account id') > -1 && result.indexOf('# end') > -1 }); }" });
console.log("Roundtrip:", r2.content[0].text);

// Check errors
const msgs = await tools.mcp_chrome_devtools.list_console_messages({});
const text = JSON.stringify(msgs);
const errIdx = text.indexOf('[error]');
console.log("Errors:", errIdx > -1 ? text.substring(errIdx, errIdx + 200) : "none");

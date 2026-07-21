await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/auth/login" });
await new Promise(r => setTimeout(r, 1000));
// Use evaluate_script to fill and submit as POST
await tools.mcp_chrome_devtools.evaluate_script({ 
  function: `() => {
    const form = document.querySelector('form');
    const u = form.querySelector('input[name="username"]');
    const p = form.querySelector('input[name="password"]');
    u.value = 'admin';
    p.value = 'password';
    // Trigger input events so any JS validation sees the values
    u.dispatchEvent(new Event('input', {bubbles: true}));
    p.dispatchEvent(new Event('input', {bubbles: true}));
    form.method = 'POST';
    form.submit();
    return 'submitted';
  }`
});
await new Promise(r => setTimeout(r, 3000));

await tools.mcp_chrome_devtools.navigate_page({ url: "http://localhost:8080/support-actions/unlock-user" });
await new Promise(r => setTimeout(r, 3000));
const snap = await tools.mcp_chrome_devtools.take_snapshot({});
console.log(snap.content[0].text);

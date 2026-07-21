/**
 * verify-sse-nav-v4.ts — Final verification of Datastar SSE navigation
 */

await tools.mcp_chrome_devtools.navigate_page({
  url: "http://localhost:8080/",
});
await tools.mcp_chrome_devtools.wait_for({
  text: ["Portal Home"],
  timeout: 5000,
});
console.log("1. Home loaded");

// Click Support Actions via dispatched event
const click1 = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    var link = document.querySelector('a[data-on-click__prevent*="support-actions"]');
    if (!link) return 'not found';
    link.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, view: window }));
    return 'clicked: ' + link.href;
  }`,
});
console.log("2. Click Support Actions:", JSON.stringify(click1));

await new Promise((r) => setTimeout(r, 3000));

const state1 = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    return JSON.stringify({
      url: window.location.pathname,
      title: document.title,
      contentStart: document.getElementById('main-content').innerHTML.substring(0, 200),
    });
  }`,
});
console.log("3. After Support Actions click:", JSON.stringify(state1));

// Now click IRM Reports
const click2 = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    var link = document.querySelector('a[data-on-click__prevent*="reports"]');
    if (!link) return 'not found';
    link.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, view: window }));
    return 'clicked: ' + link.href;
  }`,
});
console.log("4. Click Reports:", JSON.stringify(click2));

await new Promise((r) => setTimeout(r, 3000));

const state2 = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    return JSON.stringify({
      url: window.location.pathname,
      title: document.title,
      contentStart: document.getElementById('main-content').innerHTML.substring(0, 200),
    });
  }`,
});
console.log("5. After Reports click:", JSON.stringify(state2));

await tools.mcp_chrome_devtools.take_screenshot({});
console.log("6. Screenshot taken");

const msgs = await tools.mcp_chrome_devtools.list_console_messages({});
console.log("7. Console:", JSON.stringify(msgs).substring(0, 500));

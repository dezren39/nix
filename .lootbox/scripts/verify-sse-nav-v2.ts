/**
 * verify-sse-nav-v2.ts — Verify Datastar SSE fragment navigation
 */

// Navigate to the home page
await tools.mcp_chrome_devtools.navigate_page({
  url: "http://localhost:8080/",
});
await tools.mcp_chrome_devtools.wait_for({
  text: ["Portal Home"],
  timeout: 5000,
});
console.log("1. Home page loaded");

// Check Datastar is loaded
const dsCheck = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    return JSON.stringify({
      datastarAttrs: document.querySelectorAll('[data-on-click__prevent]').length,
      mainContent: !!document.getElementById('main-content'),
      navSidebar: !!document.getElementById('nav-sidebar'),
      datastarModule: !!document.querySelector('script[src*="datastar"]'),
    });
  }`,
});
console.log("2. Setup check:", JSON.stringify(dsCheck));

// Get the admin link details
const adminCheck = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    var links = document.querySelectorAll('#nav-sidebar a[data-on-click__prevent]');
    var info = [];
    links.forEach(function(l) {
      info.push({ text: l.textContent.trim(), href: l.href, dsAttr: l.getAttribute('data-on-click__prevent') });
    });
    return JSON.stringify(info);
  }`,
});
console.log("3. All Datastar nav links:", JSON.stringify(adminCheck));

// Now try clicking the Admin link via dispatching a real mouse event
const clickResult = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    var links = document.querySelectorAll('#nav-sidebar a[data-on-click__prevent]');
    var adminLink = null;
    for (var i = 0; i < links.length; i++) {
      if (links[i].textContent.trim() === 'Admin') {
        adminLink = links[i]; break;
      }
    }
    if (!adminLink) return 'Admin link not found among ' + links.length + ' links';
    
    // Dispatch a real click event to trigger Datastar
    var evt = new MouseEvent('click', { bubbles: true, cancelable: true });
    adminLink.dispatchEvent(evt);
    return 'Dispatched click on Admin link: ' + adminLink.href;
  }`,
});
console.log("4. Click admin:", JSON.stringify(clickResult));

// Wait for SSE response
await new Promise((r) => setTimeout(r, 3000));

// Check if content changed
const afterClick = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    return JSON.stringify({
      url: window.location.pathname,
      title: document.title,
      contentPreview: document.getElementById('main-content').innerHTML.substring(0, 200),
      activeLinks: Array.from(document.querySelectorAll('#nav-sidebar a.active')).map(function(a) { return a.textContent.trim(); })
    });
  }`,
});
console.log("5. After admin click:", JSON.stringify(afterClick));

// Take screenshot
await tools.mcp_chrome_devtools.take_screenshot({});
console.log("6. Screenshot taken");

// Check console for Datastar errors
const msgs = await tools.mcp_chrome_devtools.list_console_messages({});
console.log("7. Console:", JSON.stringify(msgs).substring(0, 800));

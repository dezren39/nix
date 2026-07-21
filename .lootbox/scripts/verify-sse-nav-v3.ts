/**
 * verify-sse-nav-v3.ts — Debug Datastar SSE navigation
 */

// Navigate fresh
await tools.mcp_chrome_devtools.navigate_page({
  url: "http://localhost:8080/",
});
await tools.mcp_chrome_devtools.wait_for({
  text: ["Portal Home"],
  timeout: 5000,
});
console.log("1. Home loaded");

// Clear network requests
await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    // Check what Datastar version/state we have
    var ds = window.Datastar || window.datastar;
    return JSON.stringify({
      hasDatastar: !!ds,
      windowKeys: Object.keys(window).filter(function(k) { return k.toLowerCase().indexOf('datastar') >= 0; }),
    });
  }`,
});

// Check if __prevent modifier is recognized by looking for Datastar errors
const consoleCheck = await tools.mcp_chrome_devtools.list_console_messages({});
console.log(
  "2. Console after load:",
  JSON.stringify(consoleCheck).substring(0, 600),
);

// Try clicking the Support Actions link (simple, not in a details)
const clickResult = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    var link = null;
    var links = document.querySelectorAll('#nav-sidebar a');
    for (var i = 0; i < links.length; i++) {
      if (links[i].textContent.trim() === 'Support Actions') {
        link = links[i]; break;
      }
    }
    if (!link) return 'Support Actions not found';
    
    var dsAttr = link.getAttribute('data-on-click__prevent');
    
    // Check if Datastar actually bound to this element
    // Try triggering via the actual event
    var evt = new MouseEvent('click', { bubbles: true, cancelable: true, view: window });
    var prevented = !link.dispatchEvent(evt);
    
    return JSON.stringify({
      href: link.href,
      dsAttr: dsAttr,
      prevented: prevented,
      textContent: link.textContent.trim(),
    });
  }`,
});
console.log("3. Click result:", JSON.stringify(clickResult));

// Wait for potential SSE response
await new Promise((r) => setTimeout(r, 3000));

// Check network requests for SSE
const networkReqs = await tools.mcp_chrome_devtools.list_network_requests({});
console.log(
  "4. Network requests:",
  JSON.stringify(networkReqs).substring(0, 1000),
);

// Check URL and content
const state = await tools.mcp_chrome_devtools.evaluate_script({
  function: `function() {
    return JSON.stringify({
      url: window.location.href,
      content: document.getElementById('main-content').innerHTML.substring(0, 150),
    });
  }`,
});
console.log("5. State after click:", JSON.stringify(state));

// Check console for Datastar errors
const consoleFinal = await tools.mcp_chrome_devtools.list_console_messages({});
console.log(
  "6. Final console:",
  JSON.stringify(consoleFinal).substring(0, 800),
);

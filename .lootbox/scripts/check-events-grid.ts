await tools.mcp_chrome_devtools.evaluate_script({ 
  function: `() => {
    const eventsGrid = document.getElementById('events-grid');
    const lockedGrid = document.getElementById('locked-users-grid');
    const collapse1 = document.querySelector('[data-grid-collapse]');
    const collapses = document.querySelectorAll('[data-grid-collapse]');
    const recentCard = document.querySelector('h3');
    const allH3 = [...document.querySelectorAll('h3')].map(h => h.textContent);
    return JSON.stringify({
      eventsGridExists: !!eventsGrid,
      eventsGridParent: eventsGrid?.parentElement?.className,
      eventsGridParentHidden: eventsGrid?.parentElement?.classList?.contains('hidden'),
      eventsGridHeight: eventsGrid?.offsetHeight,
      lockedGridExists: !!lockedGrid,
      lockedGridHeight: lockedGrid?.offsetHeight,
      collapseCount: collapses.length,
      collapseStates: [...collapses].map(c => ({ hidden: c.classList.contains('hidden'), height: c.offsetHeight })),
      h3s: allH3
    }, null, 2);
  }`
});

---
name: mcpx-browser
description: Use when driving a browser through chrome-devtools or a similar stateful MCP server via mcpx - navigating, clicking, screenshotting, reading network activity. Covers why browser servers need exclusive sessions and how to avoid corrupting a shared one.
---

# Driving a browser through mcpx

A browser server is **stateful**. It holds a page, a history and a session,
and two callers sharing one instance will step on each other: one navigates
away while the other is reading the DOM.

## Get your own instance

```jsonc
{ "mcpServers": { "chrome-devtools": {
    "command": "chrome-devtools-mcp",
    "mcpx": { "sharing": "exclusive", "scope": "session" } } } }
```

`exclusive` plus `session` means each agent session leases its own browser
process. Without it, concurrent agents corrupt one shared browser, and the
symptom is baffling: a click that lands on the wrong page.

mcpx learns the session from the harness, so nothing has to be passed.

## One script, not many calls

Browser work is a sequence, and each `mcpx exec` is a fresh script. Put the
whole sequence in one:

```ts
await tools.chrome_devtools.navigate_page({ url: "https://example.com" });
await tools.chrome_devtools.click({ uid: "..." });
const snap = await tools.chrome_devtools.take_snapshot({});
console.log(JSON.stringify(snap).slice(0, 2000));
```

Splitting that across three calls works, because the lease is held for the
session, but it is slower and the intermediate results all cross the wire.

## Reading what came back

Snapshots and network logs are **large**. Print a length first, then a slice:

```ts
const net = await tools.chrome_devtools.list_network_requests({});
const reqs = JSON.parse(net.content[0].text);
console.log(reqs.filter((r: any) => r.status >= 400)
                .map((r: any) => `${r.status} ${r.url}`).join("\n"));
```

Four failing requests is what you wanted. The other six hundred are not.

## When it hangs

A browser call that never returns is usually a page waiting on something.
`mcpx status` shows whether the instance is alive; `mcpx log --since 2m`
shows what it last did. `mcpx restart chrome-devtools` gets a fresh one.

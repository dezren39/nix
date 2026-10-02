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
// chrome-devtools-mcp answers in markdown text and every page tool takes the
// numeric pageId that new_page and list_pages print ("2: Title (url) [selected]").
const pages = String(await chrome_devtools.new_page({ url: "https://example.com" }));
const pageId = Number(pages.match(/^(\d+):.*\[selected\]$/m)?.[1]);
const snap = String(await chrome_devtools.take_snapshot({ pageId }));
console.log(snap.length, "chars;", snap.slice(0, 2000));
```

`click` takes a `uid` from that snapshot, so a click is snapshot, find the
element, click -- all in the same script.

Splitting that across three calls works, because the lease is held for the
session, but it is slower and the intermediate results all cross the wire.

## Reading what came back

Snapshots and network logs are **large**. Print a length first, then a slice:

```ts
const net = String(await chrome_devtools.list_network_requests({ pageId }));
// one line per request: "reqid=1 GET https://example.com/ [200]"
console.log(net.split("\n").filter((l) => /\[(failed|[45]\d\d)\b/i.test(l)).join("\n"));
```

Four failing requests is what you wanted. The other six hundred are not.

## Keeping a screenshot

```ts
const shot = await chrome_devtools.take_screenshot({ pageId, format: "png" });
const ref = await artifact("page.png", shot);   // the image, not the caption
console.log(ref.size, ref.uri);
```

`examples/screenshot-and-errors.ts` does all of the above in one script and
was run against chrome-devtools-mcp:
`mcpx run screenshot-and-errors.ts https://example.com [namespace]`.

## When it hangs

A browser call that never returns is usually a page waiting on something.
`mcpx status` shows whether the instance is alive; `mcpx log --since 2m`
shows what it last did. `mcpx restart chrome-devtools` gets a fresh one.

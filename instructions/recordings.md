# Recordings: append the moments worth remembering

When something happens in a session that you would stop and tell the user about,
**also append a JSON line to `~/git/ai_recordings.jsonl`**.

Append only. Never edit or rewrite existing lines. You do not need to read the
file first, though reading it is not forbidden.

## When

Any time you feel the need to say something like:

- "Three things from that round are worth recording"
- "The agent hit the exact trap I hit"
- "More importantly, I framed the direction wrong"
- "I told you 117/54, then 116/54 — both are stale"
- "The original method was correct; Copilot misread it, and the subagent then
  spent six rounds generating new findings while fixing a non-bug"
- "It bounded that differently from the suggestion, and better"
- "It deleted its own test after finding it could never fail"

Concretely: a significant discovery, a mistake you made, a number you got wrong,
a subagent that ignored its brief *or* followed it too literally, a review
finding that was wrong, a fix that made things worse, a tool that lied. Surprise,
guilt, irritation and relief are all signals — if you want to flag it to the
user, it belongs in the file too.

**Write it for the user as you normally would, then record it.** The recording is
in addition to the explanation, not instead of it.

## Format

One JSON object per line. There is no required schema. Ideally valid JSON;
malformed-but-structured beats nothing, and nothing beats silence.

A reasonable starting shape — add or drop fields freely:

```json
{"ts":"2026-09-30T22:41:00Z","pwd":"/path/where/it/happened","repo":"operations-portal","origin":"git@github.com:org/repo.git","kind":"short-slug","sentiment":{"positive":["measured instead of trusting the brief"],"negative":["my framing sent it to the wrong layer"]},"note":"What happened, with the evidence.","lesson":"What to do differently."}
```

`sentiment` is **any** — a dict of arrays, a bare string like `"good"`, a list of
tags, `{"fixed": true}`, whatever expresses it. Describe how you actually feel
about the event; do not flatten it to fit a schema.

Prefer specifics over summary: file:line, the quoted failure text, the command
and its real output, the number that was wrong and the number that was right.
These lines will be normalised into a database later, so detail now is cheap and
absence is permanent.

Future: see `dezren39/nix` for the tracking issue on a tool or plugin to accept
these directly.

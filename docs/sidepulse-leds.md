# SidePulse LED programs: what the hardware allows

Findings from making the SidePulse Dot show more than one thing at a time.

Everything here was verified against `sdled.wasm` — the firmware's own parser,
shipped inside the sidepulse package at `src/sidepulse/resources/sdled.wasm` —
rather than by reading the docs and hoping. Several things that the DSL
reference implies are possible turn out not to be, and one thing I claimed was
impossible turns out to be approximable. The upstream reference is
`LEDS_FORMAT.md` in the sidepulse source.

## The execution model

The host writes a program to `LEDS.LED` and walks away; the device runs it on
its own. This is why the status bar can be busy, or asleep, without the LEDs
freezing, and why there is no filesystem write per animation frame on a USB
device.

The consequence is that **everything must be expressible as a single static
program**. A host-driven real-time scheduler would mean rewriting `LEDS.LED`
every few hundred milliseconds — roughly 36,000 writes an hour to a USB flash
device, through a TCC-gated helper, on a design that deliberately suppresses
redundant writes (`led_status.py` compares against the last program it wrote).

## Hard limits

| Limit | Value |
| --- | --- |
| Program size | **512 bytes** |
| Program length | **20 physical lines** |
| Any single duration or delay | **65535 ms** |

Exceeding a limit is not a silent truncation: a parse error stops the program
and **blinks all LEDs red six times**. On a device whose red means "a run
failed", an oversized program reads as a failure that never happened. This is
why `compose.py` checks `fits()` and sheds the least-urgent signal rather than
risking the write, and why every timing value is clamped to 65535 ms on the way
in from settings.

In practice **lines are the binding constraint, not bytes**. The worst case
across every reachable combination is 339 bytes but 11 lines.

The 65535 ms cap is why the two-minute purple heartbeat is written as two
consecutive 60-second holds rather than one 120-second one.

## What works, verified

### Colour-list lines — about 40% cheaper

```text
#FF0010 #FF3A00 260ms pulse                     ← 27 chars
0:#FF0010 260ms pulse; 1:#FF3A00 260ms pulse    ← 44 chars, identical result
```

Assign by position, with duration, easing **and** delay all accepted. Colours
past the compiled LED count are syntax-checked then ignored, so one program is
portable between the 2-LED Dot and the 8-LED Pro.

### `brightness N` mid-program

Accepted anywhere, not just at the top. Intensity is therefore a second channel
alongside hue: a signal can be rendered dim for "background" and full for
"foreground", which on a two-LED device is a meaningful extra dimension that
nothing currently uses.

### `roll` — continuous circulation

```text
#FF0010 #00FF66
roll 1s linear
repeat
```

37 bytes. On two LEDs this cross-fades red↔green continuously, the two colours
trading places smoothly rather than blinking. A genuinely distinct gesture from
blink or breathe, and the cheapest program of the lot.

### Easings

`linear`, `ease`, `ease-in`, `ease-out`, `ease-in-out`, `cosine`, `pulse`,
`none`. The composer currently uses only `none`, `pulse` and `ease-out`.

`pulse` is a full envelope — it rises to the colour and returns to the line's
*start* colour within the duration. That is why breathing frames need no
trailing dark gap.

## What does not work

Each was tested directly; the notes are what the firmware actually did.

### Independent per-LED animation cycles — impossible

A line lasts as long as its longest segment, and an LED that is not reassigned
on the following line **holds its colour**. So this does not give a fast red
blink beside a slow amber breathe:

```text
0:#FF0010 120ms none 0ms; 1:#FF3A00 1.6s pulse 0ms
0:#FF0010 120ms none 300ms
repeat
```

LED 0 lights red and then sits **solid** for the remaining 1.48 s of the first
line.

### Stacking two segments for one LED on a line — impossible

```text
0:#FF0010 120ms none 0ms; 0:#00FF66 120ms none 400ms
```

Only green ever appears. "If an LED is assigned more than once on a line, the
last assignment wins." A line cannot carry a multi-step sequence for one LED.

### `off` with an LED index — parse error

```text
0:#FF0010 120ms none; 1:off 120ms none
```

Fails with `bad-index`. `off` is a device-wide keyword. Darkening one LED while
lighting another requires an explicit black, `1:#000000`.

### Loop nesting — impossible

**Exactly one `repeat` per program.**

| Program shape | Result |
| --- | --- |
| `repeat` (infinite, from line 1) | ok |
| `repeat N`, then more lines, ending held | ok |
| `repeat N` … then a final bare `repeat` | **`bad-repeat`** |
| two `repeat N` blocks | **`bad-repeat`** |

So one signal cannot be given an inner loop that cycles faster than another.
`repeat N` is only usable for one-shot sequences that end holding a colour,
which is useless for a status light that must run indefinitely.

## Consequence: why compositions use compact motifs

Because every LED steps through one shared timeline, preserving each signal's
authored cadence exactly would need a loop as long as their common multiple:

| Signal | Animation | Cycle |
| --- | --- | --- |
| Error | double blink, then a pause | 1120 ms |
| Ask | slow breathe | 1600 ms |
| Done | single blip, long fade | 790 ms |

`lcm(1120, 1600, 790)` is **884 800 ms** — about 15 minutes of timeline. Not
expressible in 20 lines.

So compositions use compact motifs on a shared frame, and a signal's authored
animation is used only when it is the only one showing — the common case, and
the one worth being exact about.

## Drift: less impossible than first stated

True drift — front and back running at genuinely independent rates, never
resyncing — is impossible, because one global loop means everything resyncs
every cycle by construction.

But relative frequency **is** controllable: you choose how many frames each
signal appears in. `split` already uses this, giving the lead LED a blink on
every frame and each other signal one frame in turn, so the most urgent signal
appears three times as often as any other when four are live.

Pushed further, a signal on frames 1,3,5,7,9,11 against another on 1,4,7,10
gives a polyrhythm that takes 12 frames to repeat — around **7 seconds** at the
default frame length, and up to ~11 seconds within the 20-line budget. To the
eye that reads as two rhythms drifting against each other. What is unavailable
is *unbounded* drift, not the appearance of it.

## The modes

Ten of them, described in full in [`sidepulse-modes.md`](./sidepulse-modes.md).
Seven are static single programs; `drift` and `live` are driven by the host and
buy cadences a static program provably cannot express.

What matters here is which DSL feature each one leans on, because that is what
the probes above were for:

| Mode | Leans on |
| --- | --- |
| `round-robin` | authored motifs, concatenated |
| `split` | per-LED indexed segments |
| `paired` | **colour-list lines** — 40% fewer bytes than indexed pairs |
| `orbit` | **`roll`**, plus an eased re-seed between revolutions |
| `depth` | **per-colour dimming** (not `brightness`, which is global) |
| `beacon` | an unassigned LED **holding its colour** through the dark half |
| `tide` | **`cosine`** chains, and per-colour dimming, with no off state |
| `drift` | host phase over a coprime frame grid |
| `live` | host phase, one-line programs |
| arrivals | **bounded `repeat N`** followed by a timed second write |

The last row is the clearest example of a limit shaping a design: exactly one
`repeat` per program means "play once then loop the composition" is not
expressible, so an arrival has to be two writes and a timer rather than one
clever program.

## Timing

A single `tempo` multiplier scales every duration in every mode, defaulting to
**2.5**. The per-mode constants read naturally as a design — 140ms blink, 420ms
gap, 900ms swell — but on an LED at the edge of vision all of them were too
quick. Each mode's numbers are tuned relative to each other and those
relationships *are* the design, so the pace lives in one dial rather than being
multiplied through and losing that legibility.

Durations are clamped to 20–65535ms **after** scaling, so a slow tempo cannot
push one past what the parser accepts.

## Verification

`tests/test_multi_signal.py` asserts that every reachable combination fits the
device, including at the extremes of the timing range, since longer durations
mean more digits and therefore more bytes. **360 programs** — 3 layouts ×
blink/breathe × every non-empty subset of 4 signals × 4 timing profiles — parse
clean against the firmware, worst case 339/512 bytes and 11/20 lines.

## Trying it

```sh
just sidepulse-led-demo            # blinking
just sidepulse-led-demo -- --breathe
```

Plays all three layouts on a real device, 30 s each with a 2 s blackout
between, against a scripted busy day that rises and falls between one and four
live signals. It stops the status-bar app for the duration and restarts it
afterwards, including on Ctrl-C. Purple is sped up so the heartbeat is visible
inside 30 seconds.

## Simulating without hardware

`sidepulse.led_wasm.SdLedWasmController` wraps the firmware parser and will
both validate a program and step it frame by frame. It needs the
`JavaScriptCore` PyObjC bindings, which are not in this flake's Python
environment; the same wasm can be driven from node by generating the shim with
`sidepulse.led_wasm._javascript_controller`. That is how every claim on this
page was checked.

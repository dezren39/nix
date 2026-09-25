# SidePulse display modes

Everything the two LEDs can be made to say, how each setting works, and why
each value is what it is.

Three documents cover this area:

- **this one** — modes, settings, colour and calibration decisions
- [`sidepulse-leds.md`](./sidepulse-leds.md) — what the hardware and its
  program format actually allow, with the probes behind each claim
- [`sidepulse-customisations.md`](./sidepulse-customisations.md) — what the
  local patches add over upstream

---

## The device is not symmetric

This governs more of the design than anything else, so it comes first.

A SidePulse Dot has two LEDs **facing opposite ways**. Standing on a desk, one
points up at you and one points down at the surface. They are not equivalent
output devices:

| | front (`0:`, upward) | back (`1:`, downward) |
| --- | --- | --- |
| seen | directly | reflected off the surface |
| at high values | **saturates to white** in the middle of the die, losing hue | — |
| at any value | — | **loses light to the surface** and picks up its colour cast |
| wants | *less* value | *more* value |
| headroom | unlimited downward | **hard ceiling at 255** |

Two consequences run through everything below.

**Nothing that dims by tier runs at full.** The front washes toward white at
high values, and white has no hue — so the most important signal would be the
one most likely to stop being identifiable. Demand-tier levels sit at
0.62–0.80 across the five modes that dim by tier (`split`, `paired`, `depth`,
`beacon`, `tide`). `orbit`, `round-robin`, `drift` and `live` emit the raw
signal colours at full and rely on the per-LED correction alone.

**The correction is applied to the front, not the back.** The obvious fix is to
boost the dim LED, and it does not work: several colours already reach 255 on
the back while still reading dim, so the correction silently stops applying and
does so *differently per colour*. Measured on the original attempt, the
intended back/front ratio was 2.08 but the delivered ratio was 1.93 for red,
2.37 for amber and 1.68 for green — which is exactly why amber looked right and
green did not. Attenuating the unconstrained side always works.

### `led_gain` and `led_channel_gain`

```json
"led_gain":         [0.48, 1.0],
"led_channel_gain": [[1.0, 0.876, 1.0], [1.0, 1.0, 1.0]]
```

| | scales | corrects | preserves hue |
| --- | --- | --- | --- |
| `led_gain` | all three channels together | brightness | **yes**, by construction |
| `led_channel_gain` | R, G, B independently | colour | no — that is the point |

`led_gain` cannot fix a *colour* difference: scaling all channels equally moves
brightness and nothing else. The surface under the device absorbs some
wavelengths more than others, which needs the channels moved independently.

**These two are the only device-specific values in the package.** They were
calibrated on one Dot, on one desk, in one room. Everything else here is a
design decision; these are a measurement, and they will not suit a different
setup. Override them in `settings.json` rather than assuming they transfer.

Comparing the two LEDs only ever constrains the **ratio** between their
corrections — multiply both by the same number and they still match each other
while both drift from the authored colour. The back is therefore pinned at 1.0
and treated as the reference, which removes the free parameter and makes the
front the only thing being determined.

---

## Signals and tiers

Modes are written against a **ranked list of signals with a tier**, never
against "red" or "error". A mode asks which signals are present and which of
them demand action; it never asks which signal is which. Adding a fifth signal,
or recolouring one, needs no change to any mode.

| Signal | Colour | Rank | Tier | Means |
| --- | --- | --- | --- | --- |
| `error` | `#FF0000` | 1 | **demand** | a run failed |
| `ask` | `#FF7A00` | 2 | **demand** | waiting on you |
| `done` | `#00FF66` | 3 | **ambient** | a run finished |
| `busy` | `#8800FF` | 4 | **ambient** | work is ongoing |

The tier is what stops modes special-casing. "Error and ask behave differently
from done and busy" is true, but it is not a fact about errors — it is a fact
about whether the signal is *asking the user to do something*. `demand` and
`ambient` name that directly, so `beacon` can say "demand blinks, ambient
glows" without mentioning a colour.

### Why these colours

**Red is `#FF0000`, not `#FF0010`.** The shipped value carried a blue tint that
pushed it toward crimson. Removing it reads as a truer red. It does not change
the separation from amber, which differs only in green.

**Amber is `#FF7A00`, not `#FF3A00`.** This is the important one. The original
pair differed by **58/255 in the green channel and nothing else**. Dimming
scales every channel equally, so at the 45% glow level that 58 became 26 — and
the back LED simply read as dim red. Two colours can stop being
*distinguishable* long before they stop being *visible*.

| amber | green channel | spread vs red at 45% |
| --- | --- | --- |
| `#FF3A00` original | 58 | 26 — one colour |
| **`#FF7A00`** | **122** | **55** |
| `#FFA000` | 160 | 72 — legible, too yellow to read as a warning |

No timing or brightness change fixes a colour-space problem. This is why
`glow_level` was also raised to an absolute 0.45: what it protects is hue, not
visibility.

---

## Brightness levels

Three, not two:

| level | meaning |
| --- | --- |
| `demand_level` | a signal asking you to act |
| *demoted demand* | an urgent signal shown as **context** — two are live and only one can blink at a time. `glow_level`, absolute rather than a fraction. |
| `ambient_level` | background information |

The middle one exists because glowing a second urgent signal at the ambient
level would call it background, which it is not; glowing it at full would
compete with the blink.

Per-LED intensity is done by **scaling the colour value**, not with the DSL's
`brightness` keyword — that is global and cannot dim one LED while another
stays lit.

---

## Modes

Select with `multi_signal.mode`. **Default: `beacon`.**

A **single** live signal always plays its own authored animation, in every
mode. Composition only engages when priority would otherwise discard
information.

`multi_signal.breathe` swaps blinks for a staggered front/back swell, but it
**only reaches four modes** — `round-robin`, `split`, `paired` and `beacon`.
The other six have no breathing variant and ignore the setting entirely:
`orbit` rolls, `tide` never goes dark, `depth` already uses `pulse`, `priority`
plays an authored animation, and the two host-driven modes are recomputed per
push.

### `beacon` — default

**What is asking for you blinks; everything else glows underneath.**

The front blinks through the demand-tier signals, one per frame, so two urgent
things both get seen rather than the second hiding behind the first. The back
never goes out: it cross-fades through the ambient signals, dimmed.

Only the front is darkened between blinks — an unassigned LED holds its colour,
so the glow survives the dark half for free.

- **Nothing urgent live** → the blink disappears and it becomes a wash.
  The device only blinks when something actually wants you. The wash stays at
  `ambient_level`; rendering it at full would mean the device got *brighter*
  the moment the urgent thing cleared.
- **No ambient live** → the demand colours glow for each other at the demoted
  level, offset by one so the two LEDs never show the same colour at once.
- **`glow_steps: 2`** — the glow takes two blink frames per colour, with an
  interpolated colour between, so the sweep runs at half the blink rate. A
  shared timeline cannot otherwise run two rates at once, and interpolating is
  what keeps it reading as travel rather than as a slower blinker.
- Urgent-on-back arrives in 40% of the frame and holds; ambient-on-back drifts
  the whole frame. A long fade spends most of its time between two hues, which
  is what makes similar colours blur.

*Chosen as the default* because it is the only layout whose structure states
the demand/ambient split outright, and the only one that stops blinking
entirely when nothing needs you.

### `split`

**Urgent in front, quiet behind, both lit, changing on a beat.**

Nothing fades and — usually — nothing goes dark. `none` snaps to the next
colour and holds it. The other layouts breathe or sweep; this one simply *is*
two colours.

Each side advances through its own pool independently, so they fall in and out
of step when the pools differ in length. Frame count is the LCM of the two.

| situation | behaviour |
| --- | --- |
| one urgent | front **blinks** 350/1050; back holds and keeps cycling |
| two urgent + ambient | both hold; front cycles urgent, back cycles quiet |
| two urgent, no ambient | **alternate flashing** across the device |
| nothing urgent | both at ambient level, offset so they show different colours |

A lone urgent colour sitting steady has no rhythm of its own and stops reading
as a demand — it becomes the background it should stand out from. Cycling
between two urgent colours supplies that rhythm by itself, so the blink is only
needed when there is nothing to cycle with.

With nothing urgent the front drops to `ambient_level` too: the whole device is
quiet and both sides should say so, or the same colour would look urgent on one
LED and not the other.

### `paired`

Two colours lit simultaneously, rotating as a sliding window:

```
red    amber
amber  green     ← amber moved forward, green enters behind
green  purple
purple red
```

Each signal enters at the back and travels to the front, seen twice per cycle,
once in each position. Fixed pairs showed the same colour in the same place
forever, which read as two alternating states rather than one rotation — the
travel is what makes it a cycle.

Written as a colour list rather than two indexed segments: identical result,
about 40% fewer bytes.

### `orbit`

Colours circulating continuously, no blink. Seeds the LEDs then `roll`s them.

The seed advances as a sliding window so **every** signal gets a turn: one seed
only ever lights two LEDs, so with four live the two least urgent would never
appear at all. Consecutive seeds share a colour, so the handover has
continuity, and the re-seed is eased (`cosine`) rather than snapping — after a
smooth revolution a snap is the only thing the eye catches.

The slowest layout at tempo 4.5. Continuous motion reads faster than discrete
blinking at the same nominal duration, because there is no dark gap for the eye
to measure the pace against.

Does **not** use tier dimming: `roll` rotates whatever the firmware already
has, so the seed is the only place to intervene.

### `depth`

Everything in turn, with **urgency as intensity** — demand 62%, ambient 15%,
a 4.1× contrast, the widest of any mode. Brightness is its only channel, so it
earns more separation than layouts that also carry urgency by position.

Uses `pulse`, which rises *and falls* within the dwell so the frame ends where
the next begins. Easing in and then cutting to black put a hard drop at the peak
of every frame — the one moment the eye was already there.

Slowest dwell of the blinking modes at tempo 4.0: dimness only reads against
something brighter, and in a whole-device rotation the only reference is the
*previous* frame. That comparison needs time, and more of it the dimmer things
get.

### `tide`

A wash that **never goes dark**, `cosine` throughout.

The two LEDs are one step apart in the sequence — the front carries where the
wash is, the back where it came from, so a colour visibly travels. Setting both
to one colour threw away the only spatial dimension a two-LED device has and
made a four-signal wash look like one colour cycling.

Colours are drawn at their tier's level, so the wash brightens as it reaches
something urgent and settles as it passes. This is the only mode that is never
off, so constant full brightness is most tiring here — and with no darkness to
punctuate it, intensity is the only thing left to say which colour matters.

**Holds its cycle, not its step** (`cycle_ms: 2700`, scaled by tide's tempo 3.0
to an 8100ms cycle). With a fixed step, four colours come round every 10.8s but
two come round every 5.4s — fewer signals would read as *busier*, which is
backwards. Dividing a target cycle by the signal count inverts that:

| signals | step | effective tempo |
| --- | --- | --- |
| 2 | 4050ms | 4.5 |
| 3 | 2700ms | 3.0 |
| 4 | 2025ms | 2.25 |

A constant cycle is strictly `1/n`, so fixing one point determines the rest.

### `round-robin`

Each signal's **compact motif** in turn on both LEDs — the authored animation
reduced to its recognisable gesture, so error's double blink runs 720ms against
its authored 1120, ask 820 against 1600 and done 610 against 790. The most
legible of the composed layouts, at the cost of frequency. Tuned by
`motif_scale` rather than hold/gap, since these are authored shapes.

### `priority`

One colour, the most urgent. The historical behaviour, kept available.

### `drift` — host-driven

Two halves of the device beating at different rates, sliding in and out of
phase. Each signal gets a period in frames by rank — **2, 3, 5, 7, coprime** —
and lights on frames divisible by it. Front carries demand, back ambient. With
four signals the pattern repeats after 2×3×5×7 = 210 frames — two frames per
push at the 700ms default, so 105 pushes, about **73 seconds**.

The device has one global loop, so everything inside it resyncs every cycle;
the longest non-repeating pattern that fits in 20 lines is about eleven
seconds. So the timeline lives on the host and is pushed every 700ms.

### `live` — host-driven

Every signal on its own unquantised clock. Where `drift` keeps a shared frame
grid, this gives each signal its authored cadence — 1120ms error, 1600ms ask,
790ms done — and asks what each LED should show *right now*. The answer is a
one-line, 49-byte program.

Host-driven modes cost one device write per tick, rate limited by `push_ms`,
with identical programs suppressed. The timer exists only while such a mode is
selected **and** two or more signals are live.

---

## Arrivals

A newly-arrived signal announces itself before joining the rotation.

| signal | announcement | length |
| --- | --- | --- |
| `error` | double blink — its authored **signature** | 700ms |
| `ask` | slow breath, held longest | 2617ms |
| `done` | slow breath | 1817ms |
| `busy` | slow breath | 1817ms |

`error` keeps its signature because the double blink says "failure" before the
colour registers. Everything else breathes: most animations are built to repeat
in the background, so replaying them verbatim is either too brief to notice
(the done blip is 110ms) or too busy.

The length is bounded at both ends: at least **700ms**, so a very short gesture
is not missed outright, and at most **5000ms** (`MAX_ARRIVAL_MS`), so no
arrival can hold the device for longer than five seconds however long its
authored animation runs. An arrival is an announcement, not a takeover.

**Every** new signal is queued, not just the most urgent. Several routinely
arrive in one snapshot — a run fails while another finishes — and announcing
only the winner means the quieter ones are never seen to arrive at all.

**Brightness follows the tier.** Demand arrivals use `demand_level`; ambient
ones are lifted halfway toward it. At their own 15–28% they would announce at a
brightness the eye ignores; at 100% they would contradict the tier system and
hit the front's saturation. The per-LED correction applies, same as everything
else — arrivals predated both mechanisms and bypassed them until this was
fixed.

Leading *and* trailing silence is stripped. Leading darkness is an animation's
periodicity — the purple heartbeat is dark for 120s then breathes once —
and trailing darkness is the rest between repetitions, which as a one-shot is
dead air. Pauses *inside* the gesture survive: the beat between the error's two
blinks is what makes it a double blink.

It uses the DSL's bounded `repeat N`. The format allows **exactly one
`repeat`**, so "play once then loop the composition" cannot be a single
program — that is why this is two writes and a timer.

Fires in every mode except `priority`, and only with 2+ signals live. Disable
with `multi_signal.arrivals: false`.

---

## Settings reference

```json
"multi_signal": {
  "mode": "beacon",
  "breathe": false,
  "arrivals": true,
  "timing": {
    "tempo": 2.5,
    "hold_ms": 140,
    "gap_ms": 420,
    "swell_ms": 900,
    "stagger_ms": 450,
    "roll_ms": 1400,
    "reseed_ms": 320,
    "push_ms": 700,
    "cycle_ms": 0,
    "glow_steps": 1,
    "motif_scale": 1.0,
    "easing": "none",
    "reseed_easing": "cosine",
    "master_brightness": 255,
    "demand_level": 1.0,
    "ambient_level": 0.28,
    "glow_level": 0.45,
    "glow_fade_urgent": 0.4,
    "glow_fade_ambient": 1.0,
    "direction": "back-to-front",
    "led_gain": [0.48, 1.0],
    "led_channel_gain": [[1.0, 0.876, 1.0], [1.0, 1.0, 1.0]],

    "beacon": { "hold_ms": 700 }
  }
}
```

Four layers, each falling through to the next: **a mode's block → your explicit
base → the built-in departure for that mode → the plain default.** A mode block
**merges** with the built-in departure rather than replacing it, key by key, so
keys you do not mention keep their built-in values — writing
`"beacon": { "hold_ms": 700 }` leaves beacon's `gap_ms`, `ease-out`,
`demand_level` and `glow_steps` exactly as they were. Setting a key at the top
level marks it explicit and drops the built-in departure for that key in every
mode, which is what makes a hand-set `tempo` or `demand_level` actually reach
the modes that had departed from it.

| key | meaning | used by |
| --- | --- | --- |
| `tempo` | scales every duration, clamped 0.25–6.0 | all |
| `hold_ms` | how long a colour is lit | split, paired, depth, beacon, tide, drift |
| `gap_ms` | dark time after a frame | split, paired, depth, beacon, drift |
| `swell_ms` / `stagger_ms` | breath length, front-to-back offset | the breathing variants of round-robin, split, paired, beacon |
| `roll_ms` / `reseed_ms` / `reseed_easing` | revolution, and the fade between seeds | orbit |
| `push_ms` | host push interval | drift, live |
| `cycle_ms` | hold the cycle constant instead of the step; 0 disables | tide |
| `glow_steps` | blink frames per glow colour | beacon |
| `motif_scale` | multiplier on authored shapes, clamped 0.25–8.0 | round-robin |
| `easing` | transition curve | all except round-robin and split, which hardcode `none` |
| `master_brightness` | global scale, emitted only below 255 | all |
| `demand_level` / `ambient_level` / `glow_level` | per-tier intensity | split, paired, depth, beacon, tide |
| `glow_fade_urgent` / `glow_fade_ambient` | fraction of the frame the glow spends travelling | beacon |
| `direction` | which way colours travel | paired, orbit, tide, beacon (its no-demand wash) |
| `led_gain` / `led_channel_gain` | per-LED correction | all |

`easing` accepts `linear`, `ease`, `ease-in`, `ease-out`, `ease-in-out`,
`cosine`, `pulse`, `none`. Anything else falls back rather than failing the
load. Durations are clamped to 20–65535 ms **after** tempo scaling, so a slow
tempo cannot push one past what the device will parse.

### Built-in departures

| mode | departs | why |
| --- | --- | --- |
| `split` | demand 0.75, ambient 0.26 | both LEDs usually lit and nothing fades, so it is the most continuously *present* layout |
| `paired` | hold 820, gap 260, `pulse`, demand 0.78, ambient 0.26 | a swell needs room to swell; at a short hold a pulse lands as a blink with soft edges |
| `orbit` | `linear`, **tempo 4.5** | continuous motion has no dark gap to measure pace against |
| `depth` | hold 700, gap 260, `pulse`, demand 0.62, ambient 0.15, **tempo 4.0** | dimness needs dwell to compare and depth to notice |
| `beacon` | hold 700, gap 260, `ease-out`, demand 0.72, `glow_steps` 2 | the glow needs room to travel; the blink is the signal so it stays brighter than depth's |
| `tide` | hold 900, `cosine`, demand 0.80, ambient 0.22, `cycle_ms` 2700, **tempo 3.0** | never dark means never resting, so both levels sit lower |

### Why `tempo` exists

The per-mode numbers read naturally as a design — 140ms blink, 420ms gap, 900ms
swell — but on an LED at the edge of vision every one was too quick. Each
mode's numbers are tuned *relative to each other*, and those relationships are
the design, so the pace lives in one multiplier rather than being multiplied
through the constants and losing that legibility.

The modes that departed upward are the ones where the eye has no hard edge to
measure against: `orbit` has no dark gap at all, `depth` asks you to compare
adjacent frames, `tide` never goes dark.

---

## Sizes and verification

Budget is **512 bytes / 20 lines**; the binding constraint is lines, not bytes.

Every composition mode × blink and breathe × every non-empty subset of signals
is checked against the budget: **288 programs, worst case 310/512 bytes and
11/20 lines.**

The check is `ComposedProgram.fits()` in Python, not a firmware parse — the
tests do not load `sdled.wasm`. The byte and line limits it enforces were
established against the real parser (see `docs/sidepulse-leds.md`), but a
program passing `fits()` has not itself been through the firmware. Arrivals
are not covered by the sweep at all.

This matters more than it sounds: an oversized program is not truncated, it is
a parse error, and the device signals that by **blinking all LEDs red six
times**. On a device whose red means "a run failed", that would read as a
failure that never happened.

So `compose_program` checks the budget before returning, and if a program will
not fit it **drops the least urgent signal and composes again**. This is the
only path by which a mode shows fewer signals than are live; at the shipped
timings it is never taken, but a hand-edited settings file can reach it.

## Trying it

```sh
just sidepulse-led-demo                                    # nine modes in turn
just sidepulse-led-demo -- --modes beacon --loop           # one, forever
just sidepulse-led-demo -- --signals error,ask,done,busy   # pin a fixed state
just sidepulse-led-demo -- --palette --gain 0.5,1.0        # calibrate by eye
just sidepulse-led-demo -- --min-signals 3 --urgent-only   # only busy states
```

`--palette` cycles solid colours on both LEDs at once, which is the way to
re-derive `led_gain` and `led_channel_gain` for a different device or surface:
both LEDs get the identical value, so every difference you see is the hardware.
White is the most revealing swatch — no hue to hide behind.

The status-bar app is stopped for the duration and restarted afterwards,
including on Ctrl-C.

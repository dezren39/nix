#!/usr/bin/env python3
"""Play the three multi-signal LED layouts on a real SidePulse device.

Each layout gets 30 seconds of a scripted "busy day" - four signals live,
occasionally dropping to two or one - with two seconds of darkness between
layouts to mark the switch.

The purple long-running-work signal is sped up for the demo. In normal use it
is a heartbeat: dark for two minutes, then one slow breathe. Waiting two
minutes per beat would make it invisible here, so `--purple-period` shortens
the cycle without changing anything else about how it looks.

The status-bar app owns the device and rewrites LEDS.LED whenever the agent
state changes, so it is stopped for the duration and restarted afterwards -
including on Ctrl-C.
"""

from __future__ import annotations

import argparse
import os
import signal
import subprocess
import sys
import time
from pathlib import Path

from dataclasses import replace

from sidepulse.compose import (
    TIER_DEMAND,
    SIGNALS,
    SIGNAL_COLORS,
    MULTI_SIGNAL_MODES,
    MULTI_SIGNAL_PRIORITY,
    SIGNAL_ASK,
    SIGNAL_BUSY,
    SIGNAL_DONE,
    SIGNAL_ERROR,
    MultiSignalTiming,
    arrival_queue,
    compose_program,
    mode_is_dynamic,
)

STATUS_BAR_JOB = "io.sidepulse.agentstatus"
BLURBS = {
    "round-robin": "each signal's own gesture, in turn, whole device",
    "split": "front and back alternate, each carrying one colour",
    "paired": "two colours lit at once, cycling through pairs",
    "orbit": "colours seeded then rolled, circulating forever",
    "depth": "everything in turn; urgency sets brightness",
    "beacon": "the urgent one blinks, the rest glow underneath",
    "tide": "a slow cosine wash that never goes dark",
    "drift": "host-driven: coprime rhythms that go out of phase",
    "live": "host-driven: every signal on its own unquantised clock",
}
LAYOUTS = tuple(
    (mode, BLURBS[mode]) for mode in MULTI_SIGNAL_MODES if mode != MULTI_SIGNAL_PRIORITY
)

# A scripted working session. Each entry is (seconds, signals).
#
# Weighted heavily toward *not changing*: the steady rotation is what you are
# actually choosing between, and arrivals are meant to be occasional
# interruptions of it. An earlier version changed state every few seconds,
# which made every layout look like a slideshow of announcements and left
# almost no time to judge the pattern itself.
#
# Arrivals are also spaced one at a time. Several signals appearing together
# queue up and play back to back, which is correct behaviour but tells you
# nothing about how a single interruption feels.
SCRIPT = (
    # Two signals, one of each tier - the ordinary state.
    (16.0, (SIGNAL_ASK, SIGNAL_BUSY)),
    (14.0, (SIGNAL_ASK, SIGNAL_DONE, SIGNAL_BUSY)),
    # Nothing urgent at all. Layouts that treat the tiers differently should
    # visibly change shape here rather than blinking the least-bad news.
    (16.0, (SIGNAL_DONE, SIGNAL_BUSY)),
    (14.0, (SIGNAL_ERROR, SIGNAL_DONE, SIGNAL_BUSY)),
    # Two urgent at once - does the second get seen, or hide behind the first?
    (16.0, (SIGNAL_ERROR, SIGNAL_ASK, SIGNAL_BUSY)),
    (14.0, (SIGNAL_ERROR, SIGNAL_ASK)),
    # The fullest state gets the longest dwell: it is the one where a layout
    # has the most to do, and the one worth staring at.
    (30.0, (SIGNAL_ERROR, SIGNAL_ASK, SIGNAL_DONE, SIGNAL_BUSY)),
    # Back to quiet, then a single urgent thing on its own.
    (14.0, (SIGNAL_DONE, SIGNAL_BUSY)),
    (12.0, (SIGNAL_ERROR, SIGNAL_BUSY)),
    (14.0, (SIGNAL_ASK, SIGNAL_DONE)),
)

# A calibration palette: the signal colours you are actually using, then the
# primaries and secondaries, then white. Shown solid on both LEDs at once, so
# the only difference you can see is the two LEDs themselves - which is what
# `led_gain` exists to correct.
PALETTE = (
    ("error", None),
    ("ask", None),
    ("done", None),
    ("busy", None),
    ("red", "#FF0000"),
    ("green", "#00FF00"),
    ("blue", "#0000FF"),
    ("yellow", "#FFFF00"),
    ("cyan", "#00FFFF"),
    ("magenta", "#FF00FF"),
    ("white", "#FFFFFF"),
)

ARRIVAL_NATIVES = {
    SIGNAL_ERROR: "off 60ms\n#FF0010 120ms\noff 120ms\n#FF0010 120ms\noff 700ms\nrepeat",
    SIGNAL_ASK: "off\n#FF3A00 1.6s pulse\nrepeat",
    SIGNAL_DONE: "off 40ms\n#00FF66 110ms\noff 640ms ease-out\nrepeat",
    SIGNAL_BUSY: "off 60s none\noff 60s none\n#8800FF 1.6s pulse\nrepeat",
}

SIGNAL_LABELS = {
    SIGNAL_ERROR: "red",
    SIGNAL_ASK: "amber",
    SIGNAL_DONE: "green",
    SIGNAL_BUSY: "purple",
}


def run_palette(target: Path, seconds: float, gain, level: float, channel_gain) -> None:
    """Hold each colour solid on both LEDs, for dialling in `led_gain`."""

    from sidepulse.compose import ModeTiming, dim, led

    clock = ModeTiming(led_gain=gain, led_channel_gain=channel_gain)
    print(f"\n=== palette   gain={gain}  level={level:.0%}   {seconds:g}s each")
    for name, literal in PALETTE:
        color = literal or SIGNAL_COLORS[name]
        shown = dim(color, level)
        front, back = led(shown, 0, clock), led(shown, 1, clock)
        program = front if front == back else f"{front} {back}"
        print(f"    {name:<9} {color}  ->  {program}")
        write_program(target, program)
        time.sleep(seconds)


def device_path(explicit: str | None) -> Path:
    if explicit:
        return Path(explicit)
    for name in ("PulseDot", "SidePulseDot", "SidePulsePro"):
        candidate = Path("/Volumes") / name
        if candidate.is_dir():
            return candidate
    raise SystemExit("No SidePulse device mounted under /Volumes.")


def write_program(target: Path, program: str) -> None:
    (target / "LEDS.LED").write_text(program + "\n", encoding="utf-8")


def launchctl(action: str) -> None:
    uid = os.getuid()
    plist = Path.home() / "Library/LaunchAgents" / f"{STATUS_BAR_JOB}.plist"
    if action == "stop":
        subprocess.run(
            ["launchctl", "bootout", f"gui/{uid}/{STATUS_BAR_JOB}"],
            capture_output=True,
        )
    else:
        subprocess.run(
            ["launchctl", "bootstrap", f"gui/{uid}", str(plist)],
            capture_output=True,
        )


def speed_up_purple(program: str, period_seconds: float) -> str:
    """The heartbeat's two-minute silence, shortened for the demo."""

    if "60s" not in program:
        return program
    gap = max(0.2, period_seconds)
    return program.replace("off 60s none\noff 60s none", f"off {gap:g}s none")


def run_layout(
    target: Path,
    layout: str,
    blurb: str,
    purple_period: float,
    breathe: bool,
    previous: tuple[str, ...] = (),
    tempo: float = 1.0,
    script=None,
) -> tuple[str, ...]:
    print(f"\n=== {layout}{' (breathing)' if breathe else ''}"
          f"{f'  tempo x{tempo:g}' if tempo != 1.0 else ''}  —  {blurb}")
    native = {
        SIGNAL_BUSY: speed_up_purple(
            "off 60s none\noff 60s none\n#8800FF 1.6s pulse\nrepeat",
            purple_period,
        )
    }
    timing = MultiSignalTiming(tempo=tempo)
    push = timing.for_mode(layout).push_ms / 1000.0
    dynamic = mode_is_dynamic(layout)
    phase = 0
    for cycle, (seconds, signals) in enumerate(script or SCRIPT, 1):
        # A newly-arrived signal announces itself first, exactly as the status
        # bar does it: one pass of its own animation, two if it wants you.
        queue = arrival_queue(previous, signals, ARRIVAL_NATIVES)
        previous = signals
        if len(signals) >= 2:
            for plan in queue:
                print(f"       ^ {SIGNAL_LABELS[plan.signal]} arriving, {plan.duration_ms}ms")
                write_program(target, plan.program)
                time.sleep(plan.duration_ms / 1000.0)
                seconds = max(0.0, seconds - plan.duration_ms / 1000.0)
        composed = compose_program(
            signals,
            mode=layout,
            breathe=breathe,
            timing=timing,
            native_programs=native,
            phase=phase,
        )
        names = "+".join(SIGNAL_LABELS[s] for s in composed.signals)
        print(
            f"    {seconds:>4.1f}s  {names:<28}"
            f"{composed.byte_length:>4}B {composed.line_count:>3}L"
            f"{'  (pushing)' if dynamic else ''}"
        )
        if not dynamic:
            write_program(target, composed.program)
            time.sleep(seconds)
            continue
        # Host-driven layouts are re-pushed on their own tick, exactly as the
        # status bar's driver does it.
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            write_program(
                target,
                compose_program(
                    signals,
                    mode=layout,
                    breathe=breathe,
                    timing=timing,
                    native_programs=native,
                    phase=phase,
                ).program,
            )
            phase += 1
            time.sleep(min(push, max(0.0, deadline - time.monotonic())))
    return previous


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--device", help="Path to the mounted device.")
    parser.add_argument(
        "--purple-period",
        type=float,
        default=2.0,
        help="Seconds of darkness between purple breaths (default 2; real life is 120).",
    )
    parser.add_argument(
        "--palette",
        action="store_true",
        help=(
            "Cycle solid colours on both LEDs instead of running a layout. "
            "Use with --gain to dial in per-LED correction by eye."
        ),
    )
    parser.add_argument(
        "--gain",
        help="Per-LED output correction, e.g. '0.85,1.25'. Default is no correction.",
    )
    parser.add_argument(
        "--channel-gain",
        help=(
            "Per-LED per-channel correction as 'r,g,b:r,g,b' (front:back), "
            "e.g. '1,1.148,1:1,1.311,1'. Corrects colour, where --gain "
            "corrects only brightness."
        ),
    )
    parser.add_argument(
        "--palette-level",
        type=float,
        default=0.72,
        help="Brightness the palette is shown at (default 0.72, the demand level).",
    )
    parser.add_argument(
        "--min-signals",
        type=int,
        default=0,
        help="Only play script steps with at least this many signals live.",
    )
    parser.add_argument(
        "--urgent-only",
        action="store_true",
        help="Only play script steps that include a demand-tier signal.",
    )
    parser.add_argument(
        "--recolour",
        action="append",
        default=[],
        metavar="SIGNAL=#RRGGBB",
        help=(
            "Override a signal's colour for this run, e.g. ask=#FFA000. "
            "Repeatable. Useful for A/B-ing hue choices on real hardware."
        ),
    )
    parser.add_argument(
        "--signals",
        help=(
            "Hold one fixed set instead of playing the script, e.g. "
            "'error,ask,done,busy'. Useful for staring at a single state."
        ),
    )
    parser.add_argument(
        "--tempo",
        type=float,
        default=1.0,
        help="Scale every duration in every layout. Above 1 is slower.",
    )
    parser.add_argument(
        "--loop",
        action="store_true",
        help="Repeat forever instead of playing through once. Ctrl-C to stop.",
    )
    parser.add_argument(
        "--modes",
        help=(
            "Comma-separated layouts to play, or 'static' for every layout the "
            "device runs unaided. Defaults to all of them."
        ),
    )
    parser.add_argument(
        "--breathe",
        action="store_true",
        help="Render every layout as the staggered front/back swell instead of blinks.",
    )
    parser.add_argument(
        "--blackout",
        type=float,
        default=2.0,
        help="Seconds of darkness between layouts (default 2).",
    )
    args = parser.parse_args()

    layouts = LAYOUTS
    if args.modes:
        if args.modes.strip() == "static":
            wanted = [m for m, _ in LAYOUTS if not mode_is_dynamic(m)]
        else:
            wanted = [m.strip() for m in args.modes.split(",") if m.strip()]
        unknown = [m for m in wanted if m not in dict(LAYOUTS)]
        if unknown:
            raise SystemExit(f"unknown layout(s): {', '.join(unknown)}")
        layouts = tuple((m, dict(LAYOUTS)[m]) for m in wanted)

    for pair in args.recolour:
        key, _, value = pair.partition("=")
        key, value = key.strip(), value.strip().upper()
        if key not in SIGNALS or not value.startswith("#") or len(value) != 7:
            raise SystemExit(f"bad --recolour: {pair!r} (want signal=#RRGGBB)")
        # Both the registry and the colour index, since layouts read either.
        SIGNALS[key] = replace(SIGNALS[key], color=value)
        SIGNAL_COLORS[key] = value
        ARRIVAL_NATIVES[key] = ARRIVAL_NATIVES.get(key, "").replace(
            SIGNAL_LABELS.get(key, ""), SIGNAL_LABELS.get(key, "")
        )
        print(f"recoloured {key} -> {value}")

    gain = (1.0, 1.0)
    if args.gain:
        try:
            gain = tuple(float(x) for x in args.gain.split(","))
        except ValueError:
            raise SystemExit(f"bad --gain: {args.gain!r} (want e.g. 0.85,1.25)")

    channel_gain = ((1.0, 1.0, 1.0), (1.0, 1.0, 1.0))
    if args.channel_gain:
        try:
            channel_gain = tuple(
                tuple(float(x) for x in part.split(","))
                for part in args.channel_gain.split(":")
            )
        except ValueError:
            raise SystemExit(f"bad --channel-gain: {args.channel_gain!r}")

    script = SCRIPT
    if args.min_signals or args.urgent_only:
        script = tuple(
            step
            for step in script
            if len(step[1]) >= args.min_signals
            and (
                not args.urgent_only
                or any(SIGNALS[key].tier == TIER_DEMAND for key in step[1])
            )
        )
        if not script:
            raise SystemExit("no script steps match those filters")
        print(f"filtered to {len(script)} step(s)")
    if args.signals:
        wanted = tuple(
            key.strip() for key in args.signals.split(",") if key.strip()
        )
        unknown = [key for key in wanted if key not in SIGNAL_LABELS]
        if unknown:
            raise SystemExit(f"unknown signal(s): {', '.join(unknown)}")
        # One long step, so the state simply holds. Arrivals still fire once on
        # the way in, which is the honest behaviour.
        script = ((3600.0, wanted),)

    target = device_path(args.device)
    print(f"device: {target}")
    print("stopping the status bar so it does not fight for the device...")
    launchctl("stop")

    def restore(*_args):
        write_program(target, "off")
        launchctl("start")
        print("\nstatus bar restarted; device handed back.")

    signal.signal(signal.SIGINT, lambda *_: (restore(), sys.exit(130)))
    if args.palette:
        try:
            while True:
                run_palette(
                    target,
                    args.blackout or 4.0,
                    gain,
                    args.palette_level,
                    channel_gain,
                )
                if not args.loop:
                    break
        finally:
            restore()
        return 0
    try:
        pass_number = 0
        carried: tuple[str, ...] = ()
        while True:
            pass_number += 1
            if args.loop and len(layouts) > 1:
                print(f"\n--- pass {pass_number} ---")
            for layout, blurb in layouts:
                if args.blackout > 0:
                    write_program(target, "off")
                    time.sleep(args.blackout)
                # Carried across passes so looping does not re-announce every
                # signal at the top of each one.
                carried = run_layout(
                    target,
                    layout,
                    blurb,
                    args.purple_period,
                    args.breathe,
                    carried,
                    args.tempo,
                    script,
                )
            if not args.loop:
                break
        if args.blackout > 0:
            write_program(target, "off")
            time.sleep(args.blackout)
    finally:
        restore()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

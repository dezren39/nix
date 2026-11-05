# Palette journal

## 2026-11-04 - Color output gating was per-script and incomplete
**Learning:** This repo's user-facing shell scripts each invented their own
ANSI color handling: opencode-share gated on a tty but ignored NO_COLOR,
brew-repair and clean.sh colored unconditionally (raw escapes landed in
piped/logged output), and full-clean bolded its step headers the same way.
**Action:** When touching terminal UX here, apply the one house gate --
colors only when that output stream is a terminal and `NO_COLOR` is unset --
to every script that emits escapes, as a set, not just the one in front of you.

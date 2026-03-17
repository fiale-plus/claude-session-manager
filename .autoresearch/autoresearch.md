# Autoresearch: TUI Visual Clarity Sweep

## Objective

Make the session+PR listings clean and scannable under load (10+ sessions, 5+ PRs).
Specifically: strip must show attention hierarchy clearly without +N overflow confusion.
Draw inspiration from Zellij's tab bar approach — compact inactive items, clear active item.

## Current State

Cycle 0/30 | Not started

### Current Strip Architecture
- Horizontal pill row: `[sess1] [sess2] ... │ [PR#1] [PR#2]`
- Overflow: `+N` on either side when budget exceeded
- Pills: 20-char truncated name + state color + pending badge
- Selected pill: inverted color (bg=state color, fg=white, underline)
- Session states: running(green), waiting(yellow), idle(gray), dead(gray)
- PR states: failing(red), running(yellow), passing(green), merged(gray)

### Known Pain Points
1. At 10+ sessions, most pills hidden behind `+7` — no state info visible
2. No visual grouping — running sessions look same as idle in overflow
3. The `│` separator between sessions/PRs is subtle and easy to miss
4. Merged/closed PRs take up same space as active ones
5. Attention-needing sessions (pending) only distinguishable when visible in strip
6. Status bar already has counts (5 running, 2 pending) but strip doesn't leverage this

## Strategy

Start with the highest-impact structural changes first. Each agent explores a different axis:
- A: Can we change the strip layout fundamentally (sidebar, rows, grouping)?
- B: Can we make the existing strip smarter (compact modes, ordering, visual hierarchy)?
- C: Can we make status bar + zoom carry more of the load?

## What Worked
(populated during cycles)

## Dead Ends
(populated during cycles)

## Next Experiments

### Agent A (Opus) — Architectural Alternatives
1. Two-row strip: sessions on row 1, PRs on row 2 — doubles density without overflow
2. State-grouped sections: `[▶▶▶] [⏸⏸] [✔]` with mini labels
3. Sidebar layout: left vertical list, main area takes remaining width
4. Summary strip: `5▶ 3⏸ 2✔ | 4✓ 1✗` → expand on hover/select

### Agent B (Sonnet) — Strip Density
1. Compact inactive pills: idle/dead → just icon (1 char) to save space
2. Attention-first ordering: pending sessions always at front of strip
3. Visual weight: pending sessions get bordered pill, others plain
4. PR filtering: hide merged/closed PRs by default, show count only

### Agent C (Sonnet) — Status Bar + Zoom
1. Status bar attention widget: flashing/colored attention count when any session pending
2. Zoom header: add mini-map of all sessions (1 glyph each) when >5 sessions
3. Strip context: show `Sessions [3▶ 2⏸]  PRs [2✓ 1✗]` instead of plain counts

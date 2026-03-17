# Autoresearch: TUI Visual Clarity Sweep

## Objective

Make the session+PR listings clean and scannable under load (10+ sessions, 5+ PRs).
Specifically: strip must show attention hierarchy clearly without +N overflow confusion.
Draw inspiration from Zellij's tab bar approach — compact inactive items, clear active item.

## Current State

Cycle 30/30 | **COMPLETE** — All 3 agents finished, 28/30 cycles kept (Agent A analyst = 10 analysis entries)

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

## Results

| Agent | Files | Cycles | All Kept | Summary |
|-------|-------|--------|----------|---------|
| A (Opus analyst) | none | 10 | n/a | 5 RECOMMEND, 3 SKIP, 2 other |
| B (Sonnet) | strip.go, pill.go, styles.go | 10 | ✓ | Strip: 295→118 chars (60% reduction) |
| C (Sonnet) | app.go, zoom.go, pr_zoom.go, hints.go, queue.go | 10 | ✓ | 10 improvements all kept |

**Build/tests: PASS** (combined result, `go build ./... && go test ./...`)

## What Worked

### Strip (Agent B)
- **Tiered pill compaction**: selected=20 chars, running/waiting=8, idle/dead=4. Biggest single space win.
- **Passive pill weight**: idle/dead get no background, just dim text. Visual hierarchy without layout change.
- **Attention-first sort**: pending→running→waiting→idle→dead. Critical sessions never scroll off left.
- **Terminal PR collapse**: merged/closed → `(+N done)` when active PRs exist. Removes done clutter.
- **Compact PR pills**: title only for failing/approved. Running/passing show `⏳ #42` = 7 chars.
- **State-group summary**: `▶3 ⏸1 ✔5 ●1` prefix at 5+ sessions. Instant fleet scan.
- **Enriched overflow**: `+4(▶2⏸1)` instead of bare `+4`. Hidden state info.
- **Dead session collapse**: `(●3)` at 8+ sessions. Removes truly-done clutter.
- **Critical PR emphasis**: failing→red dim bg, approved→green dim bg. 3-level urgency.

### Status Bar / Zoom (Agent C)
- **Session state breakdown**: `7▶ 2⏸ 1✔` replaces flat "7 running"
- **Pending badge**: `[⚡ 2 PENDING]` orange background — eye-catching alert
- **Oldest-pending age**: `[⚡ 2 PENDING] 45s ago` — urgency signal
- **Failing PR badge**: `[✗ N FAILING]` red background — consistent with pending
- **PR state breakdown**: `5 PRs 3✓ 1✗ 1⏳` mirrors session breakdown
- **Fleet map**: above session zoom, `Session 3/10: [▶]▶▶⏸⏸✔●`
- **Merge readiness**: first line of PR zoom, `✓ approved  ✓ checks (12/12)  ✓ mergeable  ⎇ squash`
- **Done-state treatment**: merged/closed PR zoom shows clean "✔ Merged" header
- **Safety grouping**: queue groups destructive vs safe tools per session

## Dead Ends
(from Agent A analysis)
- **State-grouped strip headers**: group headers waste 8-12 chars, overflow algo becomes complex at boundaries
- **Summary mode** (aggregate counts): loses individual session identity
- **Sidebar layout**: major refactor, reduces main panel width, v2 territory

## Remaining Opportunities
- Zellij-style brackets (needs visual prototyping)
- Progressive compaction (continuous rather than tiered name shortening)
- Keyboard-driven expansion popup
- PR state-color in strip separator
- Glow animation for pending pill borders (glowPos already exists)

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

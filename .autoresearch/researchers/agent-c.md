# Researcher C (Sonnet): Status Bar + Zoom Improvements

## Assignment
Implement visual improvements to app.go (status bar), zoom.go, pr_zoom.go, hints.go, queue.go.
Target: status bar that communicates session load at a glance; zoom panels that stay useful under load.
Focus: richer status bar summary, attention widgets, zoom panel quick-scan improvements.

## Scope
- tui/internal/tui/app.go
- tui/internal/tui/zoom.go
- tui/internal/tui/pr_zoom.go
- tui/internal/tui/hints.go
- tui/internal/tui/queue.go

## Findings

### Status Bar (app.go)

1. **Session state breakdown** — The flat "7 running" count was replaced with a per-state compact breakdown: `7▶ 2⏸ 1✔ 1●` with colors. Running=green, waiting=yellow, idle/dead=gray. Only non-zero states shown. Gives instant fleet overview without looking at the strip.

2. **Pending badge** — Changed `⚡ 2 pending` (inline orange text) to an orange background badge `[⚡ 2 PENDING]` with black text. Far more visually prominent; eye-catching even when looking away.

3. **Oldest-pending age** — After the pending badge, show `45s ago` indicating how long the oldest pending approval has been waiting. Urgency context: `⚡ 2 PENDING 5m ago` signals something is truly blocked.

4. **Failing PR badge** — Changed `✗ 2 failing` to red background badge `[✗ 2 FAILING]` — visually consistent with pending badge. Both urgent alerts use the same treatment.

5. **PR state breakdown** — Added compact per-state PR counts after the PR total: `5 PRs 3✓ 1✗ 1⏳`. Passing=green, failing=red, running=yellow, merged=dim gray. Mirrors session breakdown.

Full status bar under load: `██ CCC  ● connected  10 sessions  7▶ 2⏸ 1✔  5 PRs 3✓ 1✗  [⚡ 2 PENDING] 45s ago  [✗ 1 FAILING]`

### Session Zoom (zoom.go)

6. **Fleet map line** — When 3+ sessions exist, a 1-line fleet map appears above the session zoom: `Session 3/10: [▶]▶▶⏸⏸✔●`. Current session is bracketed and bold. Sessions with pending tools are shown in orange. Position indicator `3/10` tells operator where they are in the fleet.

### PR Zoom (pr_zoom.go)

7. **Merge readiness summary** — First line of PR zoom body shows quick-scan summary: `✓ approved  ✓ checks (12/12)  ✓ mergeable  ⎇ squash` or failure indicators. Lets user assess merge readiness in under 1 second.

8. **Done-state treatment** — Merged/closed PRs show `✔ Merged — no further action required` at top of body. Merge readiness summary is skipped for done PRs. Reduces clutter and visually distinguishes actionable vs. done PRs.

### Queue Panel (queue.go)

9. **Safety-grouped tools** — Pending tools grouped per session into `⚠ Destructive:` and `✓ Safe:` sections. Destructive listed first. Labels only shown when a session has both types. Operators can immediately identify which tools need careful scrutiny.

## Experiments

| Cycle | File | Change | Result |
|-------|------|--------|--------|
| 1 | app.go | Session state breakdown `7▶ 2⏸ 1✔` | keep |
| 2 | app.go | Orange badge for pending: `[⚡ 2 PENDING]` | keep |
| 3 | zoom.go, app.go | Fleet map line above session zoom | keep |
| 4 | app.go | Red badge for failing PRs: `[✗ N FAILING]` | keep |
| 5 | pr_zoom.go | Merge readiness summary line at PR zoom top | keep |
| 6 | zoom.go | Session N/total in fleet map label | keep |
| 7 | queue.go | Safety-grouped tools in queue panel | keep |
| 8 | app.go | Oldest-pending age next to pending badge | keep |
| 9 | pr_zoom.go | Done-state treatment for merged/closed PRs | keep |
| 10 | app.go | PR state breakdown `3✓ 1✗ 1⏳` in status bar | keep |

## Key Principle
All 10 cycles kept — every improvement passed build and tests. The visual language is now consistent: badge = urgent alert, breakdown = fleet state, glyphs = per-item states.

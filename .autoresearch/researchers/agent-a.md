# Researcher A (Opus): Deep Architectural Analysis

## Assignment
Analyze TUI visual clarity problem for 10+ session / 5+ PR load scenario.
Generate specific, implementation-ready proposals for the highest-impact structural changes.
No file modifications — this is pure design research. Output goes here only.

## Findings

### Cycle 1: Two-row strip (sessions on row 1, PRs on row 2)
- Verdict: RECOMMEND
- Why: Doubles available width per category. The `View()` height calculation already uses `lipgloss.Height(strip)` so a 2-row strip automatically adjusts `remainingHeight`. Only costs 1 extra line of terminal height. Can be conditional: 2 rows only when PRs exist.
- Implementation sketch: Split `renderUnifiedStrip` into two calls to a new `renderPillRow(pills, budget)` helper. Combine into single `styleStripBar` block. Separator `│` is eliminated — row separation IS the separator. Budget per row = `width - 2` (full width). With 120-char terminal, each row fits ~5 full-size pills or ~15+ compact pills.

### Cycle 2: Compact inactive pills (icon-only for idle/dead)
- Verdict: RECOMMEND
- Why: Biggest single impact change. Idle pills at 3 chars vs 24 chars = 8x space recovery per pill. 10 mixed-state sessions shrink from ~240 chars to ~120 chars, fitting a 120-char terminal without overflow. The zoom panel already provides full detail for the selected item.
- Implementation sketch: In `renderPillWithName`, add a `compact` path based on state and selection:
  - `idle`/`dead` + not selected + no pending: render icon only, no name, `Padding(0,0)` = 1-3 chars
  - `running` + not selected: render icon + first 5 chars of name = ~8 chars
  - `waiting` with pending OR selected: full pill (current behavior) = ~24 chars
  - Selected always gets full treatment regardless of state

### Cycle 3: Attention-first ordering (pending sessions sort to front)
- Verdict: RECOMMEND
- Why: Ensures urgent items are never hidden behind `+N` overflow. ~15 lines of code — a stable sort by priority in the `stateMsg` handler. The existing `selectedSID`/`selectedPRKey` tracking by ID already handles position changes after reordering.
- Implementation sketch: After `m.sessions = msg.Sessions` (app.go ~line 184), insert:
  ```go
  sort.SliceStable(m.sessions, func(i, j int) bool {
      return statePriority(m.sessions[i]) < statePriority(m.sessions[j])
  })
  ```
  Where `statePriority`: pending(0) > running(1) > waiting(2) > idle(3) > dead(4).

### Cycle 4: State-grouped strip with group headers
- Verdict: SKIP
- Why: Group headers add 8-12 chars of overhead. The overflow algorithm becomes significantly more complex when group boundaries straddle the visible window. Color differentiation (which already exists) + compact pills achieve the same information density more simply.

### Cycle 5: Hide terminal PRs (merged/closed auto-collapse)
- Verdict: RECOMMEND
- Why: 3 merged PRs at ~8 chars each + spaces = ~26 chars become a single 4-char summary indicator. Dramatic space recovery when many PRs are completed. Pairs naturally with two-row strip (Cycle 1) — the PR row stays clean.
- Implementation sketch: In `renderUnifiedStrip`, before building PR pills:
  ```go
  var activePRs, terminalPRs []client.TrackedPR
  for _, p := range prs {
      if p.State == "merged" || p.State == "closed" {
          terminalPRs = append(terminalPRs, p)
      } else {
          activePRs = append(activePRs, p)
      }
  }
  // Render activePRs as pills, append summary for terminalPRs:
  // dimStyle.Render(fmt.Sprintf("(%d done)", len(terminalPRs)))
  ```
  When the summary is selected, the zoom panel shows a list of all terminal PRs.

### Cycle 6: Zellij tab paradigm (brackets for selected, plain text for rest)
- Verdict: NEEDS_PROTOTYPING
- Why: Dropping pill backgrounds for non-selected items saves modest space (2 chars/pill) but the visual clarity is uncertain. Selected pill with brackets `[▶ myapp]` vs borderless non-selected creates strong hierarchy. The loss of dim backgrounds may make the strip look like noisy text rather than distinct items. Needs visual testing on actual terminals.

### Cycle 7: Summary mode (aggregate counts when items > threshold)
- Verdict: SKIP
- Why: Aggregate counts (`5▶ 3⏸ 2✔`) lose individual session identity. User cannot scan for a specific session without cycling through them. The threshold (e.g., 8 items) creates a jarring transition. Compact pills achieve similar density without losing identity.

### Cycle 8: Sidebar layout (vertical list replacing bottom strip)
- Verdict: SKIP
- Why: Major refactor touching every render function (`renderZoom`, `renderPRZoom`, `renderQueue`, `renderEmptyState`, `renderHelp`). Reduces main panel width by 22 chars. Introduces new scroll state. Changes navigation model fundamentally. Better suited for v2 if simpler improvements prove insufficient.

### Cycle 9: Visual weight differentiation (bordered pending, dim idle)
- Verdict: RECOMMEND
- Why: Pure styling change, ~15 lines in `renderPillWithName`. Pending pills get borders (draw eye), idle/dead pills drop background (fade out). Immediate at-a-glance scanning. Combines naturally with compact pills. Lowest implementation cost of all recommendations.
- Implementation sketch: In `renderPillWithName` (pill.go):
  ```go
  if !selected && len(s.PendingTools) > 0 {
      style = style.Border(lipgloss.RoundedBorder()).
          BorderForeground(colorOrange)
  } else if !selected && (s.State == "idle" || s.State == "dead") {
      style = lipgloss.NewStyle().Foreground(colorDimFg)
      // No background, no padding — minimal visual weight
  }
  ```

### Cycle 10: Synthesis
- Verdict: RECOMMEND (the ranked list below)
- Why: The top 3 recommendations (compact pills, visual weight, attention-first ordering) can be implemented as a single coherent PR with ~50 lines of changes. Together they solve the core problem: urgent items are visible, inactive items fade, and the strip fits 2-3x more items.

## Top Recommendations (ranked by impact x simplicity)

1. **Compact inactive pills** — idle/dead pills show icon-only (1-3 chars) instead of full name (24 chars)
   - Files: `tui/internal/tui/pill.go` (renderPillWithName), `tui/internal/tui/strip.go` (renderUnifiedStrip — pass state info)
   - Complexity: Low
   - Impact: High
   - Sketch: Add `compact` flag based on `!selected && state in (idle, dead)`. Compact pills render as `icon` with `Padding(0,0)` and `Foreground(dimColor)`. Running non-selected pills show `icon + name[:5]`.

2. **Visual weight differentiation** — pending pills get borders, idle pills lose backgrounds
   - Files: `tui/internal/tui/pill.go` (renderPillWithName)
   - Complexity: Low
   - Impact: High
   - Sketch: `if !selected && hasPending { style = style.Border(lipgloss.RoundedBorder()) }` + `if !selected && isPassive { style = lipgloss.NewStyle().Foreground(colorDimFg) }`. ~15 lines.

3. **Attention-first ordering** — pending sessions sort to front of strip
   - Files: `tui/internal/tui/app.go` (stateMsg handler)
   - Complexity: Low
   - Impact: Medium-High
   - Sketch: `sort.SliceStable(m.sessions, func(i,j) { return priority(i) < priority(j) })` after line 184. ~15 lines including the priority function.

4. **Hide terminal PRs** — merged/closed PRs collapse to `(+N done)` summary
   - Files: `tui/internal/tui/strip.go` (renderUnifiedStrip), `tui/internal/tui/app.go` (selectedPR handling)
   - Complexity: Medium
   - Impact: Medium
   - Sketch: Partition PRs into active/terminal before building pills. Render terminal count as a single styled summary entry. Summary selection shows list in zoom panel.

5. **Two-row strip** — sessions on top row, PRs on bottom row
   - Files: `tui/internal/tui/strip.go` (renderUnifiedStrip or new function)
   - Complexity: Medium
   - Impact: Medium
   - Sketch: Two calls to a `renderPillRow(pills, budget)` helper, joined vertically inside `styleStripBar`. Conditional: only 2 rows when both sessions and PRs exist. `View()` height math already adapts via `lipgloss.Height(strip)`.

## Dead Ends

- **State-grouped strip** (Cycle 4): Sounds intuitive but group headers waste 8-12 chars, overflow algorithm becomes complex at group boundaries, and color already provides grouping. Net negative.
- **Summary mode** (Cycle 7): Loses individual session identity. Users need to see which specific session is "myapp" vs "debug", not just that there are "5 running". Compact pills preserve identity.
- **Sidebar layout** (Cycle 8): Major refactor touching all render functions, reduces main panel width, changes navigation model. Correct direction for v2 but overkill for current iteration.

## Remaining Opportunities

- **Zellij-style brackets** (Cycle 6): Worth prototyping visually. If backgrounds can be dropped for non-selected pills without losing visual structure, this gives another 2 chars/pill and stronger selection contrast.
- **Progressive compaction**: Instead of a hard compact/full threshold, gradually shorten pill names as the strip gets more crowded: 20 chars → 12 → 8 → 5 → icon-only. This would require a width-aware name truncation strategy.
- **Keyboard-driven expansion**: Press a key (e.g., `e`) to temporarily expand the strip to show all pills with full names in a multi-line popup, then dismiss. Similar to how the help screen overlays the main content.
- **PR state-color in strip separator**: Instead of `│`, use a colored border between session and PR sections that reflects overall PR health (green = all passing, red = any failing, yellow = running). Zero-width visual cue.
- **Glow animation for pending**: The existing `glowPos` ping-pong animation could be applied specifically to pending pills' borders, making them pulse. Already half-built — currently used for the "running" session name character highlight.

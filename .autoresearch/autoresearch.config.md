---
metric: custom
measurement_command: "echo 'qualitative: count visual noise issues in 10-session+5-PR mock scenario'"
scope: tui/internal/tui/
mode: lab
cycles: 30
round: 1
target: "clean, scannable strip with 10+ sessions and 5+ PRs — no +N overflow confusion, clear attention hierarchy"
backpressure:
  - "cd tui && go build ./..."
  - "cd tui && go test ./..."
direction: maximize
created: 2026-03-17T00:00:00Z
prior_findings: []
---

# Autoresearch Configuration

## Problem Statement

The unified session+PR strip at the bottom gets cluttered with many items. Users running intense
sessions (10+ sessions, 5+ PRs) end up with a sea of tiny pills and "+N" overflow indicators.
The current design doesn't communicate attention priority at a glance.

## Proxy Metrics

Since this is visual/design research, measure each proposed change against:

1. **Attention clarity**: Would a user instantly see which sessions need action? (0-5)
2. **Information density**: At 10 sessions + 5 PRs, what % of items are visible vs hidden?
3. **Navigation cost**: Keystrokes to reach any item needing attention
4. **Build validity**: go build + go test pass

## Reference: Zellij Tab Bar Approach

Zellij's key ideas to consider:
- Active tab has full text + distinct style; inactive tabs are compact glyphs
- Tab bar auto-switches to number+icon mode when terminal is narrow
- Mode indicator (pane/tab/resize) is visually prominent in status bar
- Uses dedicated color zones, not just text decoration

## Lab Agent Scopes

- **Agent A (opus)**: Deep architectural alternatives — sidebar layout, state-grouped strip,
  summary mode for overflow, Zellij paradigm deep-dive
- **Agent B (sonnet)**: Strip density optimization — two-row split (sessions row / PRs row),
  compact pill variants, attention-first ordering, visual hierarchy improvements
- **Agent C (sonnet)**: Status bar + zoom improvements — status bar when >10 sessions,
  quick-scan improvements to zoom panels, pill label info selection

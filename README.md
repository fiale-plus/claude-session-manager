# Claude Control Center (CCC)

A terminal dashboard for managing multiple Claude Code sessions and pull requests. Auto-approves safe tools, blocks destructive commands, tracks CI/reviews, and auto-merges PRs -- all from one screen.

```
 @@ CCC  * connected  3 sessions  2 PRs  > 1 running  !1 pending
 ---------------------------------------------------------------
   my-project  RUNNING  [AUTO]  > feature-branch
   PID 12345  ~/repos/my-project  @ 5s

 -- Pending Approval
   ! Bash: git push --force origin main

 -- Activities
   14:23:05  @  Bash: go test ./...
   14:23:02  >  Running tests before push...
   14:22:58  @  Read: daemon/internal/state/state.go

 -- Last Output
   "All 91 tests pass. Ready to push."

 ---------------------------------------------------------------
 <-> nav | ^v scroll | Enter focus | a autopilot | y approve | h help
 > my-project  || other-proj  |  x #148 fix-auth  v #145 tests
```

Sessions and PRs share the bottom strip. Arrow left/right moves through all items. When crossing the `|` separator, the detail panel switches between session view and PR view.

## Quick install

```bash
git clone https://github.com/fiale-plus/claude-session-manager.git
cd claude-session-manager
cd daemon && /opt/homebrew/bin/go build -o csm-daemon . && ./csm-daemon install && cd ../tui && /opt/homebrew/bin/go build -o csm .
```

`csm-daemon install` handles everything: launchd service, HTTP hooks in `~/.claude/settings.json`, and Ghostty keybinds. Restart Claude Code sessions afterward for hooks to take effect.

## Run the TUI

```bash
./tui/csm
```

## Keybindings

### Navigation

| Key | Action |
|-----|--------|
| `<` `>` | Move between session/PR pills |
| `^` `v` | Scroll detail panel |
| `Home` / `End` | Jump to first / last item |
| `PgUp` / `PgDn` | Scroll 5 lines at a time |
| `Tab` | Jump to next item needing attention |
| `h` | Toggle help screen |
| `Esc` | Close overlay / cancel input |
| `q` | Quit |

### Session keys

| Key | Action |
|-----|--------|
| `Enter` | Focus -- switch to session's Ghostty tab |
| `a` | Cycle autopilot: OFF -> ON -> YOLO |
| `y` | Approve pending tool call |
| `n` | Reject pending tool call |
| `A` | Approve all pending (non-destructive) |
| `Q` | Toggle approval queue overlay |

### PR keys

| Key | Action |
|-----|--------|
| `Enter` | Open PR in browser |
| `a` | Cycle PR autopilot: OFF -> AUTO -> YOLO |
| `o` | Open PR in browser |
| `m` | Merge -- pick strategy (squash/rebase/aviator/merge commit) |
| `+` | Add PR to tracking (paste URL) |
| `-` | Remove selected PR from tracking |

## Autopilot modes

### Session autopilot

| Mode | Behavior |
|------|----------|
| **OFF** | All tools queued for manual approve/reject. |
| **ON** | Safe tools auto-approved. Destructive tools (git push, rm, --force, etc.) blocked until you approve. Unknown tools also auto-approved. |
| **YOLO** | Everything auto-approved. Destructive tools get a 10-second grace period with a blinking warning -- press `n` to reject before it fires. |

**Safe tools** -- Read, Glob, Grep, Edit, Write, Agent, and common dev commands (go, npm, pytest, cargo, git status/diff/log/commit, curl, jq, sed, etc.)

**Destructive patterns** -- `git push`, `rm`, `git reset --hard`, `git clean`, `kill`, `DROP`, `DELETE FROM`, `--force`, `--no-verify`, `npm publish`, `cargo publish`.

**Compound commands** (`&&`, `||`, `;`, `|`) are split and each part classified independently. If any part is destructive, the whole command is destructive.

### PR autopilot

| Mode | Behavior |
|------|----------|
| **OFF** | Manual everything. No auto-merge, no hammer. |
| **AUTO** | Hammer failing CI (up to 3 attempts). Auto-merge when checks pass + at least one approval. |
| **YOLO** | Same as AUTO but skips the human approval requirement -- merges on green checks alone. |

## PR tracking

### Adding and removing PRs

Press `+` in the TUI and paste a GitHub PR URL (e.g. `https://github.com/owner/repo/pull/123`). Press `-` to remove the selected PR.

Tracked PRs are persisted in `~/.csm/prs.json` and survive daemon restarts.

### CI monitoring

The daemon polls GitHub every 30 seconds via `gh pr view` to fetch check status, reviews, and mergeability. The TUI shows:

- **Checks** -- pass/fail/running status with duration
- **Reviews** -- approval state, reviewer comments
- **Timeline** -- state transitions, merge attempts, hammer attempts

### Auto-merge

When a tracked PR meets the auto-merge conditions (all checks pass, mergeable, and approval requirements met per autopilot mode), the daemon triggers merge automatically.

Merge strategies available via `m` key or auto-merge config:

| Strategy | Command |
|----------|---------|
| **Squash** (default) | `gh pr merge --squash --auto` |
| **Rebase** | `gh pr merge --rebase --auto` |
| **Aviator** | `gh pr comment --body "/aviator merge"` |
| **Merge commit** | `gh pr merge --merge --auto` |

### Hammer mode

When enabled (default for AUTO/YOLO), failing CI checks trigger automatic fix attempts. The daemon logs each attempt in the PR timeline and caps at 3 attempts (configurable via `max_hammer`).

## Architecture

```
Claude Code --HTTP POST--> csm-daemon (launchd, always running)
                             |
                             |-- hookserver: receives PreToolUse/SessionStart/SessionEnd
                             |-- classifier: safe/destructive/unknown
                             |-- state: session registry, autopilot, pending queue
                             |-- pr poller: gh CLI, 30s interval
                             |-- ghostty: tab correlation via AppleScript
                             |-- notify: macOS desktop notifications
                             |
                            \|/
                         ctlserver (Unix socket)
                             |
                            \|/
                       csm TUI (Bubble Tea)
                             |-- subscribes to state stream
                             |-- sends approve/reject/toggle commands
                             |-- renders sessions + PRs
```

### Source layout

```
claude-session-manager/
|-- daemon/                    Go daemon (launchd service)
|   |-- main.go                CLI: run, install, uninstall
|   +-- internal/
|       |-- hookserver/        HTTP + Unix socket for CC hooks
|       |-- ctlserver/         Unix socket for TUI control
|       |-- state/             Session registry, autopilot, pending queue
|       |-- classifier/        Tool safety rules (safe/destructive/unknown)
|       |-- scanner/           Process discovery + JSONL parsing
|       |-- pr/                PR model, poller, auto-merge, hammer
|       |-- ghostty/           Tab correlation + switching via AppleScript
|       |-- model/             Session + approval data types
|       +-- notify/            macOS desktop notifications
|-- tui/                       Go TUI (Bubble Tea)
|   |-- main.go
|   +-- internal/
|       |-- client/            Daemon Unix socket client
|       +-- tui/               UI: app, strip, zoom, pr_zoom, hints, pill, queue, styles
+-- plugin/                    CC hook script (curl to daemon)
```

### Daemon endpoints

| Endpoint | Purpose |
|----------|---------|
| `http://127.0.0.1:19380/hooks` | CC HTTP hooks (PreToolUse, SessionStart, SessionEnd) |
| `/tmp/csm.sock` | Legacy Unix socket for hooks |
| `/tmp/csm-ctl.sock` | TUI control (subscribe, approve, reject, toggle, PR commands) |

## Configuration

### Hook installation

`csm-daemon install` adds this to `~/.claude/settings.json`:

```json
{
  "hooks": {
    "PreToolUse": [{
      "matcher": "*",
      "hooks": [{ "type": "http", "url": "http://127.0.0.1:19380/hooks", "timeout": 90 }]
    }],
    "SessionStart": [{
      "matcher": "*",
      "hooks": [{ "type": "http", "url": "http://127.0.0.1:19380/hooks", "timeout": 5 }]
    }]
  }
}
```

### Persisted state (`~/.csm/`)

| File | Contents |
|------|----------|
| `autopilot.json` | Session autopilot modes (survives restarts) |
| `prs.json` | Tracked PRs with autopilot, hammer, merge config |

### Environment variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `CSM_CTL_SOCK` | `/tmp/csm-ctl.sock` | Override TUI socket path |

## Uninstall

```bash
cd daemon && ./csm-daemon uninstall
```

Removes the launchd service, CC plugin, HTTP hooks from `~/.claude/settings.json`, and Ghostty keybinds/shader.

## Requirements

- macOS (launchd, AppleScript for Ghostty integration)
- Go 1.21+
- [Ghostty](https://ghostty.org) terminal (for tab correlation and Enter-to-focus)
- Claude Code with hooks support
- `gh` CLI (for PR tracking)

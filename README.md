# Claude Session Manager (CSM)

Monitor and control multiple Claude Code sessions from a single terminal dashboard. Auto-approve safe tools, catch destructive commands before they run, and switch between sessions instantly.

```
██ CSM  ● connected  3 sessions  ▶ 1 running
────────────────────────────────────────────────
  my-project  RUNNING  ⚙ AUTO  ▸ feature-branch
  PID 12345  /Users/me/project  ⏱ 5s
── Activities
  10:05:03  ⚙  Bash: npm test
  10:05:01  ✎  Running the test suite...
── Last Output
  "All 42 tests passed."
────────────────────────────────────────────────
←→↑↓ navigate │ Enter focus │ a autopilot │ h help
 ▶ my-project   ⏸ other-project   ⏸ api-server
```

## How it works

```
Claude Code ──HTTP hooks──→ CSM Daemon (always running)
                             ├── auto-approves safe tools
                             ├── blocks destructive commands
                             └── streams state to TUI
                                      ↕
                              CSM TUI (Go terminal app)
```

1. **Daemon** runs at login via launchd, listens for CC hook events
2. **HTTP hooks** in `~/.claude/settings.json` route every tool call through the daemon
3. **Autopilot** classifies tools as safe/destructive/unknown and decides automatically
4. **TUI** connects to the daemon, shows all sessions, lets you approve/reject/toggle

## Install

```bash
# Prerequisites
brew install go

# Clone and build
git clone https://github.com/fiale-plus/claude-session-manager.git
cd claude-session-manager
cd daemon && go build -o csm-daemon . && ./csm-daemon install
cd ../tui && go build -o csm .
```

`csm-daemon install` does everything:
- Installs launchd service (daemon starts at login)
- Adds HTTP hooks to `~/.claude/settings.json`
- Installs Ghostty integration (shader + keybinds)

Restart your Claude Code sessions for hooks to take effect.

## Run the TUI

```bash
cd tui && ./csm
```

## Keybindings

| Key | Action |
|-----|--------|
| `←` `→` | Navigate between session pills |
| `↑` `↓` | Scroll detail panel |
| `Home` / `End` | Jump to first / last session |
| `PgUp` / `PgDn` | Scroll 5 lines at a time |
| `Enter` | Focus — switch to session's Ghostty tab |
| `a` | Cycle autopilot: OFF → ON → YOLO → OFF |
| `y` | Approve pending tool call |
| `n` | Reject pending tool call |
| `A` | Approve all pending (non-destructive) |
| `Q` | Toggle approval queue overlay |
| `h` | Toggle help screen |
| `q` | Quit |

## Autopilot modes

| Mode | Behavior |
|------|----------|
| **OFF** | All tools go to pending queue — manual approve/reject |
| **ON** | Safe tools auto-approved. Destructive tools blocked until you approve |
| **YOLO** | Everything auto-approved. Destructive tools get a 10-second grace period with blinking warning — press `n` to reject before it fires |

### What's "safe" vs "destructive"?

**Safe** — read-only tools (Read, Glob, Grep) and common dev commands (git status, npm test, pytest, cargo build, etc.)

**Destructive** — `git push`, `rm`, `git reset --hard`, `kill`, `DROP TABLE`, `npm publish`, `--force`, `--no-verify`, etc.

**Compound commands** (`&&`, `||`, `;`, `|`) are split and each part checked independently. If any part is destructive, the whole command is destructive.

## Architecture

```
claude-session-manager/
├── daemon/                 # Go daemon (launchd service)
│   ├── main.go             # CLI: run, install, uninstall
│   └── internal/
│       ├── hookserver/     # HTTP + Unix socket for CC hooks
│       ├── ctlserver/      # Unix socket for TUI control
│       ├── state/          # Session registry, autopilot, pending queue
│       ├── classifier/     # Tool safety rules
│       ├── scanner/        # Process discovery + JSONL parsing
│       ├── ghostty/        # Tab correlation via AppleScript
│       └── notify/         # macOS desktop notifications
├── tui/                    # Go TUI (Bubble Tea)
│   ├── main.go
│   └── internal/
│       ├── client/         # Daemon socket client
│       └── tui/            # UI components
└── plugin/                 # CC hook script (curl to daemon)
```

### Daemon ports & sockets

| Endpoint | Purpose |
|----------|---------|
| `http://127.0.0.1:19380/hooks` | CC HTTP hooks (PreToolUse, SessionStart) |
| `/tmp/csm.sock` | Legacy Unix socket for hooks |
| `/tmp/csm-ctl.sock` | TUI control (subscribe, approve, toggle) |

## Uninstall

```bash
cd daemon && ./csm-daemon uninstall
```

Removes the launchd service, plugin files, and Ghostty integration.

To also remove the HTTP hooks, delete the `"hooks"` section from `~/.claude/settings.json`.

## Requirements

- macOS (launchd, AppleScript for Ghostty tab switching)
- Go 1.21+
- Ghostty terminal (for tab correlation and Enter-to-focus)
- Claude Code with hooks support

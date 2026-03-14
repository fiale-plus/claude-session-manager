# Shipping CSM as a Ghostty Plugin — Research & Feasibility

> **Status**: Research / RFC
> **Date**: 2026-03-14
> **Context**: Ghostty 1.3.0 (released March 9, 2026)

---

## Executive Summary

Ghostty does **not yet have a formal plugin system**. Mitchell Hashimoto has confirmed a
"generic cross-platform scripting/plugin API" is on the roadmap but is a separate effort
from the scripting API, with no public timeline. However, there are **three viable
integration paths today** and one forward-looking path that CSM can pursue.

**Recommended approach**: Ship CSM as a **Ghostty-native companion** using AppleScript
automation (macOS) + custom shader status indicator (cross-platform), with the daemon
installable via Homebrew. Position for the eventual plugin API when it lands.

---

## 1. Current Ghostty Extensibility Surface

### 1.1 AppleScript Automation (macOS, Ghostty 1.3+)

Ghostty 1.3 exposes a full AppleScript dictionary (`Ghostty.sdef`):

- **Windows**: enumerate, create, focus
- **Tabs**: enumerate all tabs, get properties (id, name, index, selected)
- **Splits**: create, navigate
- **Terminals**: get focused terminal, working directory, send text/key/mouse input
- **Surface configuration**: reusable config records for `new window`, `new tab`, `split`

This is what CSM already uses in `daemon/internal/ghostty/ghostty.go` for tab
correlation. The current implementation queries `properties of every tab` and matches
by working directory.

**What this unlocks for CSM**:
- Tab-name enrichment (already implemented)
- Auto-focus a tab when its session needs approval
- Inject approval commands directly into a terminal
- Create dedicated CSM status tabs/splits
- Broadcast notifications to specific terminal sessions

**Limitations**:
- macOS only (not available on Linux/GTK builds)
- Preview feature — API may have breaking changes in 1.4
- Requires TCC permission prompt on first use

### 1.2 Custom Shaders (Cross-platform)

Ghostty supports GLSL custom shaders that render as post-processing passes over the
terminal content. As of 1.3, shaders receive rich uniform variables:

| Uniform              | Type     | Description                           |
|----------------------|----------|---------------------------------------|
| `iResolution`        | `vec3`   | Viewport resolution in pixels         |
| `iTime`              | `float`  | Time since shader loaded (seconds)    |
| `iTimeDelta`         | `float`  | Time between frames                   |
| `iFrame`             | `int`    | Frame counter                         |
| `iChannel0`          | `sampler2D` | Terminal content texture           |
| `ghostty_cursor_pos` | `vec2`   | Cursor position                       |
| `ghostty_cursor_color`| `vec4`  | Cursor color                          |
| `ghostty_cursor_shape`| `int`   | Cursor shape enum                     |
| `ghostty_color_scheme`| `int`   | Light/dark scheme                     |

**What this could unlock for CSM**:
A status-bar shader that renders a thin strip (e.g. 2-4px) at the top or bottom of
the terminal with a color that indicates session state:
- Green pulse = running
- Amber = waiting for approval
- Red flash = destructive tool pending

The shader would read from a small file (`/tmp/csm-shader-state`) that CSM updates,
but **shaders cannot read external files** — they only have access to the uniforms
listed above. So this path is limited to static visual effects, not dynamic data.

**Realistic shader use**: A "Ghostty theme" that ships alongside CSM for aesthetics,
not for functional integration.

### 1.3 Shell Integration & Keybinds

Ghostty has deep shell integration (bash, zsh, fish, elvish) and configurable keybinds:

```
# ghostty config
keybind = ctrl+shift+a=text:\x1b[csm-approve\n
```

This can inject escape sequences or text that a running CSM TUI could intercept.

**What this unlocks for CSM**:
- Global keybinds for approve/reject without switching to the CSM TUI
- `ctrl+shift+a` → approve, `ctrl+shift+r` → reject
- The keybind sends text to the focused terminal, so CSM would need a
  lightweight listener or the TUI itself running in a split

### 1.4 libghostty (Future — Not Ready)

libghostty-vt is in public alpha (Zig API available, C API coming). The long-term
vision includes:

- **libghostty-vt**: Terminal sequence parsing + state management
- **libghostty-render**: GPU rendering (Metal/OpenGL surfaces)
- **Swift/GTK frameworks**: Full terminal view widgets

This is meant for **embedding Ghostty inside other apps**, not for extending Ghostty
itself. CSM doesn't need to embed a terminal — it needs to observe and control
existing ones.

**Verdict**: Not relevant for CSM's use case until a plugin API materializes.

---

## 2. Integration Strategies

### Strategy A: Ghostty Companion Bundle (Recommended)

Ship CSM as a **Ghostty companion** — not a plugin, but a purpose-built integration:

```
brew install csm
csm install    # installs daemon + CC plugin + Ghostty config
```

The `csm install` command would:

1. **Install CC hooks plugin** → `~/.claude/plugins/csm/` (already implemented)
2. **Install Ghostty keybinds** → append to `~/.config/ghostty/config`:
   ```
   # CSM — Claude Session Manager
   keybind = ctrl+shift+y=text:\x01csm:approve\n
   keybind = ctrl+shift+n=text:\x01csm:reject\n
   keybind = ctrl+shift+q=text:\x01csm:queue\n
   ```
3. **Install Ghostty shader** (optional aesthetic):
   ```
   custom-shader = ~/.config/ghostty/shaders/csm-status.glsl
   ```
4. **Install launchd plist** (macOS) or systemd unit (Linux) for daemon
5. **Configure AppleScript automation** for tab correlation + focus

Directory structure:
```
~/.config/ghostty/
├── config                        # user's config (we append keybinds)
└── shaders/
    └── csm-status.glsl           # optional aesthetic shader

~/.claude/plugins/csm/
├── plugin.json
└── hooks/
    ├── hooks.json
    └── csm-hook.sh

~/.csm/
├── autopilot.json                # persisted state
└── csm-daemon.log
```

### Strategy B: Ghostty Split-Pane Dashboard

Use AppleScript to create a dedicated CSM split pane inside Ghostty:

```applescript
tell application "Ghostty"
    tell front window
        tell focused terminal of front tab
            split down with command "csm-tui"
        end tell
    end tell
end tell
```

This gives users a persistent CSM dashboard inside their Ghostty window without
needing a separate terminal window. Add a keybind to toggle it:

```
keybind = ctrl+shift+m=text:\x01csm:toggle-split\n
```

**Pros**: Feels native, no window switching
**Cons**: Takes terminal real estate, macOS only

### Strategy C: Wait for Plugin API

Ghostty's eventual plugin API will likely support:
- Custom UI panels/widgets (the status bar widget discussion #2421)
- Event hooks (terminal focus, command execution)
- External process communication

CSM could ship as a first-class Ghostty plugin once this lands. But the timeline
is uncertain and likely 6-12+ months away.

**Recommendation**: Don't wait. Ship Strategy A now, adopt the plugin API later.

---

## 3. Concrete Deliverables

### Phase 1: Ghostty Config Integration (This PR)

- [ ] Add `csm install --ghostty` flag to daemon installer
- [ ] Generate Ghostty keybind config snippet
- [ ] Ship a CSM aesthetic shader (subtle, non-functional)
- [ ] Update install/uninstall to manage Ghostty config

### Phase 2: AppleScript Deep Integration

- [ ] Auto-focus Ghostty tab when approval is needed
- [ ] Create CSM split pane via AppleScript
- [ ] Tab-name enrichment with session state (e.g. "myproject [▶ RUNNING]")

### Phase 3: Cross-Platform Parity

- [ ] Linux: D-Bus integration for desktop notifications
- [ ] Linux: systemd user unit for daemon
- [ ] Linux: Ghostty GTK keybind config (same format, different path)

### Phase 4: Native Plugin (When Available)

- [ ] Adopt Ghostty plugin API when it ships
- [ ] Status bar widget showing session count + pending approvals
- [ ] Native event hooks replacing the scanner polling

---

## 4. What CSM Already Has

The existing codebase already has strong Ghostty integration foundations:

| Feature                        | Status       | File                                |
|--------------------------------|--------------|-------------------------------------|
| Tab correlation via AppleScript | Working     | `daemon/internal/ghostty/ghostty.go`|
| CC hooks plugin                | Working     | `plugin/hooks/`                     |
| launchd install                | Working     | `daemon/main.go`                    |
| Unix socket daemon             | Working     | `daemon/internal/hookserver/`       |
| TUI with Bubble Tea            | Working     | `tui/go/`                           |

The gap is primarily in the **install experience** and **Ghostty-specific config
generation**.

---

## 5. References

- [Ghostty AppleScript Docs](https://ghostty.org/docs/features/applescript)
- [Ghostty 1.3.0 Release Notes](https://ghostty.org/docs/install/release-notes/1-3-0)
- [Libghostty Is Coming — Mitchell Hashimoto](https://mitchellh.com/writing/libghostty-is-coming)
- [Ghostty Config Reference](https://ghostty.org/docs/config/reference)
- [Ghostty Keybind Reference](https://ghostty.org/docs/config/keybind/reference)
- [Status Bar Widget Discussion #2421](https://github.com/ghostty-org/ghostty/discussions/2421)
- [Scripting API Discussion #2353](https://github.com/ghostty-org/ghostty/discussions/2353)
- [Ghostty Custom Shaders](https://catskull.net/fun-with-ghostty-shaders.html)
- [ghostty-workspace — YAML-driven layouts](https://github.com/manonstreet/ghostty-workspace)

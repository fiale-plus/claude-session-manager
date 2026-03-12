"""AppleScript bridge for Ghostty terminal tab management."""

from __future__ import annotations

import logging
import subprocess
import time
from dataclasses import dataclass

log = logging.getLogger(__name__)

CACHE_TTL_SECS = 10

_tab_cache: list[GhosttyTab] | None = None
_tab_cache_time: float = 0.0


@dataclass
class GhosttyTab:
    id: str
    name: str
    index: int
    selected: bool
    working_directory: str


def _run_osascript(script: str) -> str | None:
    """Run an AppleScript via osascript and return stdout, or None on error."""
    try:
        result = subprocess.run(
            ["osascript", "-e", script],
            capture_output=True,
            text=True,
            timeout=5,
        )
        if result.returncode != 0:
            log.debug("osascript failed: %s", result.stderr.strip())
            return None
        return result.stdout.strip()
    except subprocess.TimeoutExpired:
        log.debug("osascript timed out")
        return None
    except FileNotFoundError:
        log.debug("osascript not found")
        return None


_ENUMERATE_SCRIPT = '''\
tell application "Ghostty"
    set w to front window
    set tabList to every tab of w
    set output to ""
    repeat with t in tabList
        set tProps to properties of t
        set tId to id of tProps
        set tName to name of tProps
        set tIndex to index of tProps
        set tSelected to selected of tProps
        set term to focused terminal of t
        set termProps to properties of term
        set tDir to working directory of termProps
        set output to output & tId & "\\t" & tName & "\\t" & tIndex & "\\t" & tSelected & "\\t" & tDir & "\\n"
    end repeat
    return output
end tell
'''


def get_tabs(*, force: bool = False) -> list[GhosttyTab]:
    """Get all tabs from the frontmost Ghostty window.

    Results are cached for CACHE_TTL_SECS to avoid hammering AppleScript
    on every poll cycle.
    """
    global _tab_cache, _tab_cache_time

    now = time.monotonic()
    if not force and _tab_cache is not None and (now - _tab_cache_time) < CACHE_TTL_SECS:
        return _tab_cache

    raw = _run_osascript(_ENUMERATE_SCRIPT)
    if raw is None:
        _tab_cache = []
        _tab_cache_time = now
        return []

    tabs: list[GhosttyTab] = []
    for line in raw.splitlines():
        line = line.strip()
        if not line:
            continue
        parts = line.split("\t")
        if len(parts) < 5:
            continue
        tab_id, name, index_str, selected_str, working_dir = (
            parts[0],
            parts[1],
            parts[2],
            parts[3],
            parts[4],
        )
        try:
            index = int(index_str)
        except ValueError:
            index = 0
        selected = selected_str.lower() == "true"
        tabs.append(GhosttyTab(
            id=tab_id,
            name=name,
            index=index,
            selected=selected,
            working_directory=working_dir,
        ))

    _tab_cache = tabs
    _tab_cache_time = now
    return tabs


def switch_to_tab(tab_name: str) -> bool:
    """Switch to a Ghostty tab by its name.

    Uses System Events to click the radio button in Ghostty's tab group.
    Returns True if the AppleScript executed successfully.
    """
    # Escape double quotes in tab name for AppleScript string
    safe_name = tab_name.replace("\\", "\\\\").replace('"', '\\"')
    script = (
        'tell application "System Events" to tell process "Ghostty"\n'
        f'    click radio button "{safe_name}" of tab group 1 of window 1\n'
        "end tell"
    )
    result = _run_osascript(script)
    return result is not None


def invalidate_cache() -> None:
    """Force the next get_tabs() call to re-query Ghostty."""
    global _tab_cache, _tab_cache_time
    _tab_cache = None
    _tab_cache_time = 0.0

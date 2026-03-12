"""Session discovery — process scan + JSONL mapping."""

from __future__ import annotations

import logging
import time
from pathlib import Path

import psutil

from csm import ghostty
from csm.models import Session, SessionState
from csm.parser import (
    detect_state,
    extract_activities,
    extract_motivation,
    extract_session_metadata,
    read_tail,
)

log = logging.getLogger(__name__)

CLAUDE_PROJECTS_DIR = Path.home() / ".claude" / "projects"
RECENT_THRESHOLD_HOURS = 24


def encode_project_path(path: str) -> str:
    """Convert an absolute path to Claude's encoded project directory name.

    Example: /Users/pavel/repos/foo -> -Users-pavel-repos-foo
    """
    # Strip trailing slash, replace / with -
    clean = path.rstrip("/")
    if not clean:
        return "-"
    return clean.replace("/", "-")


def _find_claude_processes() -> list[dict]:
    """Find running Claude Code CLI processes.

    Returns list of dicts with keys: pid, cwd, cmdline.
    """
    results = []
    for proc in psutil.process_iter(["pid", "name", "cmdline", "cwd"]):
        try:
            info = proc.info
            cmdline = info.get("cmdline") or []
            cmdline_str = " ".join(cmdline)

            # Look for Claude CLI process patterns
            # The CLI shows up as node running claude
            if not any("claude" in arg.lower() for arg in cmdline if isinstance(arg, str)):
                continue

            # Skip ourselves and other unrelated claude matches
            if "claude-session-manager" in cmdline_str:
                continue

            cwd = info.get("cwd") or ""
            if not cwd:
                continue

            results.append({
                "pid": info["pid"],
                "cwd": cwd,
                "cmdline": cmdline,
            })
        except (psutil.NoSuchProcess, psutil.AccessDenied, psutil.ZombieProcess):
            continue

    return results


def _find_latest_jsonl(project_dir: Path) -> Path | None:
    """Find the most recently modified .jsonl file in a project directory."""
    if not project_dir.is_dir():
        return None

    jsonl_files = list(project_dir.glob("*.jsonl"))
    if not jsonl_files:
        return None

    return max(jsonl_files, key=lambda p: p.stat().st_mtime)


def _session_from_jsonl(jsonl_path: Path, pid: int | None = None) -> Session | None:
    """Build a Session from a JSONL file path."""
    try:
        entries = read_tail(jsonl_path, n_lines=50)
    except (OSError, PermissionError):
        return None

    if not entries:
        return None

    metadata = extract_session_metadata(entries)
    session_id = metadata.get("sessionId", jsonl_path.stem)
    slug = metadata.get("slug", "")
    cwd = metadata.get("cwd", "")
    git_branch = metadata.get("gitBranch", "")

    project_name = Path(cwd).name if cwd else jsonl_path.parent.name

    if pid is not None:
        state = detect_state(entries)
    else:
        state = SessionState.DEAD

    activities = extract_activities(entries)
    motivation = extract_motivation(entries)

    last_activity_time = None
    if activities:
        last_activity_time = activities[-1].timestamp

    return Session(
        session_id=session_id,
        slug=slug,
        project_path=cwd,
        project_name=project_name,
        jsonl_path=jsonl_path,
        state=state,
        last_activity_time=last_activity_time,
        activities=activities,
        last_text=motivation,
        pid=pid,
        git_branch=git_branch,
    )


def discover_sessions() -> list[Session]:
    """Discover all Claude Code sessions — running and recently dead.

    1. Scan processes for running Claude instances.
    2. Map each to its JSONL log file.
    3. Also find recently-modified JSONL files without running processes (dead sessions).
    """
    sessions: list[Session] = []
    seen_jsonl: set[Path] = set()

    # --- Phase 1: Running processes ---
    # Group processes by cwd to deduplicate (multiple node processes per session)
    procs = _find_claude_processes()
    cwd_to_pid: dict[str, int] = {}
    for proc_info in procs:
        cwd = proc_info["cwd"]
        pid = proc_info["pid"]
        # Keep the highest PID per cwd (likely the most recent / main process)
        if cwd not in cwd_to_pid or pid > cwd_to_pid[cwd]:
            cwd_to_pid[cwd] = pid

    for cwd, pid in cwd_to_pid.items():
        encoded = encode_project_path(cwd)
        project_dir = CLAUDE_PROJECTS_DIR / encoded

        jsonl_path = _find_latest_jsonl(project_dir)
        if jsonl_path is None:
            continue

        # Deduplicate by JSONL path (different cwds could map to same file)
        if jsonl_path in seen_jsonl:
            continue

        session = _session_from_jsonl(jsonl_path, pid=pid)
        if session is None:
            continue

        sessions.append(session)
        seen_jsonl.add(jsonl_path)

    # --- Phase 2: Dead/historical sessions ---
    if CLAUDE_PROJECTS_DIR.is_dir():
        cutoff = time.time() - RECENT_THRESHOLD_HOURS * 3600
        for project_dir in CLAUDE_PROJECTS_DIR.iterdir():
            if not project_dir.is_dir():
                continue

            jsonl_path = _find_latest_jsonl(project_dir)
            if jsonl_path is None:
                continue

            if jsonl_path in seen_jsonl:
                continue

            # Only include if recently modified
            if jsonl_path.stat().st_mtime < cutoff:
                continue

            session = _session_from_jsonl(jsonl_path, pid=None)
            if session is None:
                continue

            sessions.append(session)

    # --- Phase 3: Correlate with Ghostty tabs ---
    _correlate_ghostty_tabs(sessions)

    # Sort: running first, then by last activity time descending
    state_order = {
        SessionState.RUNNING: 0,
        SessionState.WAITING: 1,
        SessionState.IDLE: 2,
        SessionState.DEAD: 3,
    }
    sessions.sort(
        key=lambda s: (
            state_order.get(s.state, 9),
            -(s.last_activity_time.timestamp() if s.last_activity_time else 0),
        )
    )

    return sessions


def _correlate_ghostty_tabs(sessions: list[Session]) -> None:
    """Match sessions to Ghostty tabs by comparing working directories."""
    try:
        tabs = ghostty.get_tabs()
    except Exception:
        log.debug("Failed to get Ghostty tabs", exc_info=True)
        return

    if not tabs:
        return

    # Build a lookup: normalized working_directory -> tab
    dir_to_tab: dict[str, ghostty.GhosttyTab] = {}
    for tab in tabs:
        wd = tab.working_directory.rstrip("/")
        if wd:
            dir_to_tab[wd] = tab

    for session in sessions:
        project_path = session.project_path.rstrip("/")
        if not project_path:
            continue
        tab = dir_to_tab.get(project_path)
        if tab is not None:
            session.ghostty_tab_name = tab.name
            session.ghostty_tab_index = tab.index

"""JSONL tail reader and entry parser for Claude Code session logs."""

from __future__ import annotations

import json
import os
from datetime import datetime, timezone
from pathlib import Path

from csm.models import Activity, ActivityType, SessionState


def read_tail(jsonl_path: str | Path, n_lines: int = 20) -> list[dict]:
    """Read the last N lines from a JSONL file using backward seek.

    Efficient for large files — seeks from EOF backward instead of
    reading the entire file.
    """
    path = Path(jsonl_path)
    if not path.exists():
        return []

    file_size = path.stat().st_size
    if file_size == 0:
        return []

    # For small files, just read the whole thing
    if file_size < 64 * 1024:
        return _read_all_lines(path, n_lines)

    return _read_tail_seek(path, n_lines, file_size)


def _read_all_lines(path: Path, n_lines: int) -> list[dict]:
    entries = []
    with open(path, "r") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                entries.append(json.loads(line))
            except json.JSONDecodeError:
                continue
    return entries[-n_lines:]


def _read_tail_seek(path: Path, n_lines: int, file_size: int) -> list[dict]:
    # Start with a chunk size, grow if needed
    chunk_size = min(8192, file_size)
    lines: list[str] = []

    with open(path, "rb") as f:
        offset = file_size
        leftover = b""

        while len(lines) < n_lines + 1 and offset > 0:
            read_size = min(chunk_size, offset)
            offset -= read_size
            f.seek(offset)
            chunk = f.read(read_size) + leftover

            parts = chunk.split(b"\n")
            # First part may be partial — save for next iteration
            leftover = parts[0]
            # Rest are complete lines (last one is empty string after trailing \n)
            lines = [p.decode("utf-8", errors="replace") for p in parts[1:] if p.strip()] + lines

            chunk_size = min(chunk_size * 2, 1024 * 1024)

        # Don't forget the leftover at the very beginning of the file
        if leftover.strip():
            lines = [leftover.decode("utf-8", errors="replace")] + lines

    # Parse the last n_lines
    entries = []
    for line in lines[-n_lines:]:
        line = line.strip()
        if not line:
            continue
        try:
            entries.append(json.loads(line))
        except json.JSONDecodeError:
            continue
    return entries


def _parse_timestamp(ts: str | None) -> datetime | None:
    if not ts:
        return None
    try:
        # Handle ISO format with Z suffix
        ts = ts.replace("Z", "+00:00")
        return datetime.fromisoformat(ts)
    except (ValueError, TypeError):
        return None


def _is_tool_result(entry: dict) -> bool:
    """Check if a user entry is a tool_result (not a real user message)."""
    if entry.get("type") != "user":
        return False
    msg = entry.get("message", {})
    content = msg.get("content", [])
    if isinstance(content, list):
        return any(
            isinstance(c, dict) and c.get("type") == "tool_result"
            for c in content
        )
    return False


def _has_tool_use(entry: dict) -> bool:
    """Check if an assistant entry contains tool_use blocks."""
    if entry.get("type") != "assistant":
        return False
    content = entry.get("message", {}).get("content", [])
    return any(
        isinstance(c, dict) and c.get("type") == "tool_use"
        for c in content
    )


def _get_meaningful_entries(entries: list[dict]) -> list[dict]:
    """Filter to meaningful entry types (user, assistant, system), skip progress."""
    return [
        e for e in entries
        if e.get("type") in ("user", "assistant", "system")
    ]


def detect_state(entries: list[dict]) -> SessionState:
    """Detect session state from parsed JSONL entries.

    Logic:
    - system with subtype=turn_duration as last meaningful entry -> WAITING
    - system with subtype=stop_hook_summary (no subsequent turn_duration) -> WAITING
    - assistant with tool_use, no subsequent user tool_result -> RUNNING
    - user (not tool_result) as last -> RUNNING (assistant is working)
    - Default -> IDLE
    """
    meaningful = _get_meaningful_entries(entries)
    if not meaningful:
        return SessionState.IDLE

    last = meaningful[-1]

    # System turn_duration -> turn finished, waiting for user
    if last.get("type") == "system" and last.get("subtype") == "turn_duration":
        return SessionState.WAITING

    # System stop_hook_summary -> turn finished, waiting for user
    if last.get("type") == "system" and last.get("subtype") == "stop_hook_summary":
        return SessionState.WAITING

    # Walk backward to find the latest assistant or non-tool-result user
    for entry in reversed(meaningful):
        t = entry.get("type")

        if t == "assistant" and _has_tool_use(entry):
            # Check if there's a subsequent tool_result for this tool_use
            tool_ids = set()
            for c in entry.get("message", {}).get("content", []):
                if isinstance(c, dict) and c.get("type") == "tool_use":
                    tool_ids.add(c.get("id"))

            # Look for matching tool_results after this entry
            idx = meaningful.index(entry)
            has_result = False
            for later in meaningful[idx + 1:]:
                if _is_tool_result(later):
                    later_content = later.get("message", {}).get("content", [])
                    if isinstance(later_content, list):
                        for c in later_content:
                            if isinstance(c, dict) and c.get("tool_use_id") in tool_ids:
                                has_result = True
                                break
                if has_result:
                    break

            if not has_result:
                return SessionState.RUNNING
            continue

        if t == "user" and not _is_tool_result(entry):
            # Real user message as last meaningful -> assistant should be working
            if entry == last:
                return SessionState.RUNNING
            break

        if t == "assistant":
            # Assistant text without tool_use -> done, waiting
            if entry == last:
                return SessionState.WAITING
            break

    return SessionState.IDLE


def extract_motivation(entries: list[dict]) -> str:
    """Find the last assistant text content block. This is the 'motivation' —
    what the assistant was last saying/thinking about."""
    for entry in reversed(entries):
        if entry.get("type") != "assistant":
            continue
        content = entry.get("message", {}).get("content", [])
        for block in reversed(content):
            if isinstance(block, dict) and block.get("type") == "text":
                text = block.get("text", "")
                return text[:500]
    return ""


def extract_activities(entries: list[dict], n: int = 8) -> list[Activity]:
    """Parse recent entries into an activity timeline."""
    activities: list[Activity] = []
    meaningful = _get_meaningful_entries(entries)

    for entry in meaningful:
        ts = _parse_timestamp(entry.get("timestamp"))
        if ts is None:
            continue

        t = entry.get("type")

        if t == "assistant":
            content = entry.get("message", {}).get("content", [])
            for block in content:
                if not isinstance(block, dict):
                    continue
                if block.get("type") == "tool_use":
                    tool_name = block.get("name", "?")
                    tool_input = block.get("input", {})
                    # Build a brief summary from tool input
                    brief = _summarize_tool_input(tool_name, tool_input)
                    activities.append(Activity(
                        timestamp=ts,
                        activity_type=ActivityType.TOOL_USE,
                        summary=f"{tool_name}: {brief}",
                        detail=json.dumps(tool_input)[:500],
                    ))
                elif block.get("type") == "text":
                    text = block.get("text", "")
                    activities.append(Activity(
                        timestamp=ts,
                        activity_type=ActivityType.TEXT,
                        summary=text[:60],
                        detail=text[:500],
                    ))
                elif block.get("type") == "thinking":
                    # Skip thinking blocks — too verbose, not user-facing
                    pass

        elif t == "user" and not _is_tool_result(entry):
            msg = entry.get("message", {})
            content = msg.get("content", "")
            if isinstance(content, str):
                summary = content[:60]
            elif isinstance(content, list):
                # Concatenate text parts
                texts = [
                    c.get("text", "") if isinstance(c, dict) else str(c)
                    for c in content
                ]
                summary = " ".join(texts)[:60]
            else:
                summary = str(content)[:60]
            activities.append(Activity(
                timestamp=ts,
                activity_type=ActivityType.USER_MESSAGE,
                summary=summary,
            ))

        elif t == "system":
            subtype = entry.get("subtype", "")
            activities.append(Activity(
                timestamp=ts,
                activity_type=ActivityType.SYSTEM,
                summary=subtype,
            ))

    return activities[-n:]


def _summarize_tool_input(tool_name: str, tool_input: dict) -> str:
    """Create a short summary of tool input args."""
    if not isinstance(tool_input, dict):
        return ""

    # Common patterns
    if "command" in tool_input:
        cmd = str(tool_input["command"])
        return cmd[:60]
    if "file_path" in tool_input:
        return str(tool_input["file_path"])
    if "pattern" in tool_input:
        return str(tool_input["pattern"])[:40]
    if "query" in tool_input:
        return str(tool_input["query"])[:40]
    if "description" in tool_input:
        return str(tool_input["description"])[:40]

    # Fallback: first key=value
    for k, v in tool_input.items():
        return f"{k}={str(v)[:30]}"
    return ""


def extract_pending_tool(entries: list[dict]) -> tuple[str, dict] | None:
    """Find a tool_use that has no matching tool_result.

    Returns (tool_name, tool_input) or None.
    Needed for autopilot to know what's pending approval.
    """
    # Collect all tool_use IDs and their info
    pending: dict[str, tuple[str, dict]] = {}

    meaningful = _get_meaningful_entries(entries)
    for entry in meaningful:
        if entry.get("type") == "assistant":
            content = entry.get("message", {}).get("content", [])
            for block in content:
                if isinstance(block, dict) and block.get("type") == "tool_use":
                    tool_id = block.get("id", "")
                    pending[tool_id] = (block.get("name", "?"), block.get("input", {}))

        elif entry.get("type") == "user" and _is_tool_result(entry):
            content = entry.get("message", {}).get("content", [])
            if isinstance(content, list):
                for block in content:
                    if isinstance(block, dict) and block.get("type") == "tool_result":
                        result_id = block.get("tool_use_id", "")
                        pending.pop(result_id, None)

    if not pending:
        return None

    # Return the last pending tool
    last_id = list(pending.keys())[-1]
    return pending[last_id]


def extract_session_metadata(entries: list[dict]) -> dict:
    """Extract session metadata from entries.

    Looks for sessionId, slug, cwd, gitBranch from user-type entries
    (or any entry that carries these top-level fields).
    """
    metadata: dict = {}

    for entry in reversed(entries):
        if not metadata.get("sessionId") and entry.get("sessionId"):
            metadata["sessionId"] = entry["sessionId"]
        if not metadata.get("slug") and entry.get("slug"):
            metadata["slug"] = entry["slug"]
        if not metadata.get("cwd") and entry.get("cwd"):
            metadata["cwd"] = entry["cwd"]
        if not metadata.get("gitBranch") and entry.get("gitBranch"):
            metadata["gitBranch"] = entry["gitBranch"]

        # Stop once we have everything
        if all(k in metadata for k in ("sessionId", "slug", "cwd", "gitBranch")):
            break

    return metadata

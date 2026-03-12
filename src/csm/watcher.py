"""Incremental file tailing with position tracking.

Replaces re-reading the last N lines from scratch each poll cycle.
On each poll, checks file size — only reads new bytes when size increases.
Falls back to full read_tail() for initial load.
"""

from __future__ import annotations

import json
import logging
import os
from pathlib import Path

from csm.models import Session
from csm.parser import read_tail
from csm.scanner import discover_sessions

log = logging.getLogger(__name__)

# How many tail lines to read on first encounter of a session.
INITIAL_TAIL_LINES = 50


class Watcher:
    """Efficient session poller that tracks file positions for incremental reads.

    Instead of calling ``read_tail()`` every 2 s for every JSONL file, the
    watcher remembers the byte offset it last read to.  On subsequent polls it
    only reads the *new* bytes appended since then and parses just those lines.

    Usage::

        watcher = Watcher()
        sessions = watcher.poll()      # first call: full discovery + tail
        sessions = watcher.poll()      # subsequent: incremental reads
    """

    def __init__(self) -> None:
        # jsonl_path (str) -> last-read byte position
        self._positions: dict[str, int] = {}
        # jsonl_path (str) -> list of parsed JSONL entries (rolling tail)
        self._entry_cache: dict[str, list[dict]] = {}
        # Maximum cached entries per file (keeps memory bounded).
        self._max_cached = 120

    def poll(self) -> list[Session]:
        """Run a full discovery cycle with incremental tailing.

        Returns the session list exactly like ``discover_sessions()`` but
        cheaper on repeated calls because only new JSONL bytes are read.
        """
        sessions = discover_sessions()

        for session in sessions:
            self._update_entries(session)

        return sessions

    def _update_entries(self, session: Session) -> None:
        """Read new JSONL entries for *session* incrementally."""
        jsonl_path = str(session.jsonl_path)

        try:
            current_size = os.path.getsize(jsonl_path)
        except OSError:
            return

        last_pos = self._positions.get(jsonl_path)

        if last_pos is None:
            # First encounter — do a full tail read and record position.
            self._initial_load(session, jsonl_path, current_size)
            return

        if current_size == last_pos:
            # File unchanged — nothing to do.
            return

        if current_size < last_pos:
            # File was truncated (session restart?) — re-read from scratch.
            log.debug("JSONL file truncated: %s — re-reading", jsonl_path)
            self._initial_load(session, jsonl_path, current_size)
            return

        # File grew — read only the new bytes.
        self._incremental_read(session, jsonl_path, last_pos, current_size)

    def _initial_load(
        self, session: Session, jsonl_path: str, current_size: int
    ) -> None:
        """Full tail read for a newly-discovered or truncated file."""
        try:
            entries = read_tail(jsonl_path, n_lines=INITIAL_TAIL_LINES)
        except (OSError, PermissionError):
            entries = []

        self._entry_cache[jsonl_path] = entries
        self._positions[jsonl_path] = current_size

    def _incremental_read(
        self,
        session: Session,
        jsonl_path: str,
        start: int,
        end: int,
    ) -> None:
        """Read bytes from *start* to *end* and append parsed entries."""
        new_entries: list[dict] = []
        try:
            with open(jsonl_path, "rb") as f:
                f.seek(start)
                raw = f.read(end - start)
        except OSError:
            self._positions[jsonl_path] = end
            return

        for line in raw.split(b"\n"):
            line = line.strip()
            if not line:
                continue
            try:
                new_entries.append(json.loads(line))
            except json.JSONDecodeError:
                continue

        # Merge into cache.
        cached = self._entry_cache.get(jsonl_path, [])
        cached.extend(new_entries)
        # Trim to max.
        if len(cached) > self._max_cached:
            cached = cached[-self._max_cached :]
        self._entry_cache[jsonl_path] = cached
        self._positions[jsonl_path] = end

    def get_entries(self, jsonl_path: str | Path) -> list[dict]:
        """Return cached entries for a JSONL path (useful for callers
        that need parsed data without re-reading the file)."""
        return list(self._entry_cache.get(str(jsonl_path), []))

    def forget(self, jsonl_path: str | Path) -> None:
        """Drop tracking state for a file (e.g. when a session disappears)."""
        key = str(jsonl_path)
        self._positions.pop(key, None)
        self._entry_cache.pop(key, None)

    def clear(self) -> None:
        """Reset all tracking state."""
        self._positions.clear()
        self._entry_cache.clear()

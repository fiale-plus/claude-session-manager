"""Tests for csm.watcher — incremental file tailing with position tracking."""

from __future__ import annotations

import json
import os
import tempfile
from pathlib import Path

import pytest

from csm.watcher import Watcher


@pytest.fixture
def jsonl_file(tmp_path):
    """Create a temporary JSONL file with some initial entries."""
    path = tmp_path / "session.jsonl"
    entries = [
        {"type": "user", "message": {"content": "hello"}, "timestamp": "2026-03-12T10:00:00Z"},
        {"type": "assistant", "message": {"content": [{"type": "text", "text": "Hi!"}]}, "timestamp": "2026-03-12T10:00:01Z"},
    ]
    with open(path, "w") as f:
        for entry in entries:
            f.write(json.dumps(entry) + "\n")
    return path


class TestWatcherPositionTracking:
    """Tests for byte-position tracking in the Watcher."""

    def test_initial_load_records_position(self, jsonl_file):
        watcher = Watcher()
        watcher._initial_load(None, str(jsonl_file), os.path.getsize(jsonl_file))

        assert str(jsonl_file) in watcher._positions
        assert watcher._positions[str(jsonl_file)] == os.path.getsize(jsonl_file)

    def test_initial_load_populates_cache(self, jsonl_file):
        watcher = Watcher()
        watcher._initial_load(None, str(jsonl_file), os.path.getsize(jsonl_file))

        entries = watcher.get_entries(jsonl_file)
        assert len(entries) == 2
        assert entries[0]["type"] == "user"
        assert entries[1]["type"] == "assistant"

    def test_incremental_read_appends_new_entries(self, jsonl_file):
        watcher = Watcher()
        initial_size = os.path.getsize(jsonl_file)
        watcher._initial_load(None, str(jsonl_file), initial_size)

        # Append a new entry
        new_entry = {"type": "system", "subtype": "turn_duration", "timestamp": "2026-03-12T10:00:02Z"}
        with open(jsonl_file, "a") as f:
            f.write(json.dumps(new_entry) + "\n")

        new_size = os.path.getsize(jsonl_file)
        watcher._incremental_read(None, str(jsonl_file), initial_size, new_size)

        entries = watcher.get_entries(jsonl_file)
        assert len(entries) == 3
        assert entries[-1]["type"] == "system"
        assert entries[-1]["subtype"] == "turn_duration"

    def test_incremental_read_updates_position(self, jsonl_file):
        watcher = Watcher()
        initial_size = os.path.getsize(jsonl_file)
        watcher._initial_load(None, str(jsonl_file), initial_size)

        # Append data
        with open(jsonl_file, "a") as f:
            f.write(json.dumps({"type": "user", "message": {"content": "more"}}) + "\n")

        new_size = os.path.getsize(jsonl_file)
        watcher._incremental_read(None, str(jsonl_file), initial_size, new_size)

        assert watcher._positions[str(jsonl_file)] == new_size

    def test_no_read_when_size_unchanged(self, jsonl_file):
        """When file size hasn't changed, _update_entries should be a no-op."""
        watcher = Watcher()
        initial_size = os.path.getsize(jsonl_file)
        watcher._positions[str(jsonl_file)] = initial_size
        watcher._entry_cache[str(jsonl_file)] = [{"type": "user"}]

        # Simulate calling _update_entries — should not change cache
        from unittest.mock import MagicMock
        session = MagicMock()
        session.jsonl_path = jsonl_file
        watcher._update_entries(session)

        # Cache should be unchanged (still the single entry we put there)
        assert len(watcher._entry_cache[str(jsonl_file)]) == 1

    def test_truncated_file_triggers_full_reload(self, jsonl_file):
        """If file shrinks (truncated), do a full re-read."""
        watcher = Watcher()
        watcher._positions[str(jsonl_file)] = 999999  # Pretend we read far ahead

        from unittest.mock import MagicMock
        session = MagicMock()
        session.jsonl_path = jsonl_file
        watcher._update_entries(session)

        # Should have done a full reload — position should match actual file size
        assert watcher._positions[str(jsonl_file)] == os.path.getsize(jsonl_file)
        entries = watcher.get_entries(jsonl_file)
        assert len(entries) == 2

    def test_cache_trimmed_to_max(self, tmp_path):
        """Entries exceeding _max_cached should be trimmed."""
        watcher = Watcher()
        watcher._max_cached = 5

        path = tmp_path / "big.jsonl"
        # Write 3 entries initially
        with open(path, "w") as f:
            for i in range(3):
                f.write(json.dumps({"type": "user", "idx": i}) + "\n")

        initial_size = os.path.getsize(path)
        watcher._initial_load(None, str(path), initial_size)

        # Append 4 more entries (total 7, exceeds max of 5)
        with open(path, "a") as f:
            for i in range(3, 7):
                f.write(json.dumps({"type": "user", "idx": i}) + "\n")

        new_size = os.path.getsize(path)
        watcher._incremental_read(None, str(path), initial_size, new_size)

        entries = watcher.get_entries(path)
        assert len(entries) == 5
        # Should have the last 5 entries (indices 2..6)
        assert entries[0]["idx"] == 2
        assert entries[-1]["idx"] == 6


class TestWatcherForgetAndClear:
    def test_forget_removes_tracking(self, jsonl_file):
        watcher = Watcher()
        watcher._initial_load(None, str(jsonl_file), os.path.getsize(jsonl_file))

        watcher.forget(jsonl_file)
        assert str(jsonl_file) not in watcher._positions
        assert str(jsonl_file) not in watcher._entry_cache

    def test_clear_resets_all(self, jsonl_file):
        watcher = Watcher()
        watcher._initial_load(None, str(jsonl_file), os.path.getsize(jsonl_file))

        watcher.clear()
        assert watcher._positions == {}
        assert watcher._entry_cache == {}


class TestWatcherGetEntries:
    def test_returns_copy(self, jsonl_file):
        """get_entries should return a copy, not the internal list."""
        watcher = Watcher()
        watcher._initial_load(None, str(jsonl_file), os.path.getsize(jsonl_file))

        entries1 = watcher.get_entries(jsonl_file)
        entries2 = watcher.get_entries(jsonl_file)
        assert entries1 == entries2
        assert entries1 is not entries2

    def test_unknown_path_returns_empty(self):
        watcher = Watcher()
        assert watcher.get_entries("/nonexistent/path.jsonl") == []


class TestWatcherIncrementalMalformedJson:
    def test_skips_malformed_lines(self, jsonl_file):
        """Malformed JSON lines in the incremental read should be silently skipped."""
        watcher = Watcher()
        initial_size = os.path.getsize(jsonl_file)
        watcher._initial_load(None, str(jsonl_file), initial_size)

        # Append one bad line and one good line
        with open(jsonl_file, "a") as f:
            f.write("not valid json\n")
            f.write(json.dumps({"type": "system", "subtype": "ok"}) + "\n")

        new_size = os.path.getsize(jsonl_file)
        watcher._incremental_read(None, str(jsonl_file), initial_size, new_size)

        entries = watcher.get_entries(jsonl_file)
        # 2 original + 1 valid new = 3 (bad line skipped)
        assert len(entries) == 3
        assert entries[-1]["subtype"] == "ok"


class TestWatcherMultipleFiles:
    def test_tracks_multiple_files_independently(self, tmp_path):
        watcher = Watcher()

        file_a = tmp_path / "a.jsonl"
        file_b = tmp_path / "b.jsonl"

        with open(file_a, "w") as f:
            f.write(json.dumps({"type": "user", "file": "a"}) + "\n")
        with open(file_b, "w") as f:
            f.write(json.dumps({"type": "user", "file": "b"}) + "\n")

        watcher._initial_load(None, str(file_a), os.path.getsize(file_a))
        watcher._initial_load(None, str(file_b), os.path.getsize(file_b))

        assert len(watcher.get_entries(file_a)) == 1
        assert len(watcher.get_entries(file_b)) == 1
        assert watcher.get_entries(file_a)[0]["file"] == "a"
        assert watcher.get_entries(file_b)[0]["file"] == "b"

        # Append to file_a only
        initial_a = os.path.getsize(file_a)
        with open(file_a, "a") as f:
            f.write(json.dumps({"type": "assistant", "file": "a2"}) + "\n")
        watcher._incremental_read(None, str(file_a), initial_a, os.path.getsize(file_a))

        assert len(watcher.get_entries(file_a)) == 2
        assert len(watcher.get_entries(file_b)) == 1  # unchanged

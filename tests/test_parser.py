"""Tests for csm.parser — state detection, motivation, activities, pending tools."""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from csm.models import ActivityType, SessionState
from csm.parser import (
    detect_state,
    extract_activities,
    extract_motivation,
    extract_pending_tool,
    extract_session_metadata,
    read_tail,
)

FIXTURES = Path(__file__).parent / "fixtures"


def _load_fixture(name: str) -> list[dict]:
    path = FIXTURES / name
    entries = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if line:
                entries.append(json.loads(line))
    return entries


# --- read_tail ---


class TestReadTail:
    def test_reads_all_lines_from_small_file(self):
        entries = read_tail(FIXTURES / "sample_session.jsonl", n_lines=50)
        assert len(entries) == 6

    def test_reads_last_n_lines(self):
        entries = read_tail(FIXTURES / "sample_session.jsonl", n_lines=2)
        assert len(entries) == 2
        assert entries[-1]["type"] == "system"

    def test_nonexistent_file_returns_empty(self):
        entries = read_tail("/nonexistent/path.jsonl")
        assert entries == []


# --- detect_state ---


class TestDetectState:
    def test_waiting_after_turn_duration(self):
        entries = _load_fixture("sample_session.jsonl")
        state = detect_state(entries)
        assert state == SessionState.WAITING

    def test_running_with_pending_tool(self):
        entries = _load_fixture("running_tool_pending.jsonl")
        state = detect_state(entries)
        assert state == SessionState.RUNNING

    def test_waiting_after_assistant_text(self):
        entries = _load_fixture("waiting_after_text.jsonl")
        state = detect_state(entries)
        assert state == SessionState.WAITING

    def test_running_after_user_message(self):
        # Simulate: user just sent a message, assistant hasn't replied
        entries = [
            {
                "type": "user",
                "message": {"role": "user", "content": "Do something"},
                "timestamp": "2026-03-12T10:00:00.000Z",
                "uuid": "u1",
            }
        ]
        state = detect_state(entries)
        assert state == SessionState.RUNNING

    def test_idle_on_empty_entries(self):
        state = detect_state([])
        assert state == SessionState.IDLE

    def test_idle_on_only_progress_entries(self):
        entries = [
            {"type": "progress", "data": {"type": "hook_progress"}, "timestamp": "2026-03-12T10:00:00.000Z"},
        ]
        state = detect_state(entries)
        assert state == SessionState.IDLE

    def test_waiting_after_stop_hook_summary(self):
        entries = [
            {
                "type": "user",
                "message": {"role": "user", "content": "hello"},
                "timestamp": "2026-03-12T10:00:00.000Z",
                "uuid": "u1",
            },
            {
                "type": "assistant",
                "message": {"content": [{"type": "text", "text": "Hi!"}]},
                "timestamp": "2026-03-12T10:00:01.000Z",
                "uuid": "a1",
            },
            {
                "type": "system",
                "subtype": "stop_hook_summary",
                "timestamp": "2026-03-12T10:00:02.000Z",
                "uuid": "s1",
            },
        ]
        state = detect_state(entries)
        assert state == SessionState.WAITING


# --- extract_motivation ---


class TestExtractMotivation:
    def test_finds_last_assistant_text(self):
        entries = _load_fixture("sample_session.jsonl")
        motivation = extract_motivation(entries)
        assert "hello.py" in motivation
        assert "hello world" in motivation

    def test_empty_on_no_text(self):
        entries = [
            {"type": "user", "message": {"content": "hi"}, "timestamp": "2026-03-12T10:00:00.000Z"},
        ]
        assert extract_motivation(entries) == ""

    def test_truncates_long_text(self):
        entries = [
            {
                "type": "assistant",
                "message": {"content": [{"type": "text", "text": "x" * 1000}]},
                "timestamp": "2026-03-12T10:00:00.000Z",
            },
        ]
        motivation = extract_motivation(entries)
        assert len(motivation) == 500


# --- extract_activities ---


class TestExtractActivities:
    def test_parses_activities_from_fixture(self):
        entries = _load_fixture("sample_session.jsonl")
        activities = extract_activities(entries)
        assert len(activities) > 0

        types = [a.activity_type for a in activities]
        assert ActivityType.USER_MESSAGE in types
        assert ActivityType.TEXT in types
        assert ActivityType.TOOL_USE in types

    def test_tool_use_summary_format(self):
        entries = _load_fixture("sample_session.jsonl")
        activities = extract_activities(entries)
        tool_acts = [a for a in activities if a.activity_type == ActivityType.TOOL_USE]
        assert len(tool_acts) == 1
        assert tool_acts[0].summary.startswith("Write:")

    def test_respects_n_limit(self):
        entries = _load_fixture("sample_session.jsonl")
        activities = extract_activities(entries, n=2)
        assert len(activities) <= 2

    def test_system_entries_included(self):
        entries = _load_fixture("sample_session.jsonl")
        activities = extract_activities(entries, n=20)
        sys_acts = [a for a in activities if a.activity_type == ActivityType.SYSTEM]
        assert len(sys_acts) == 1
        assert sys_acts[0].summary == "turn_duration"


# --- extract_pending_tool ---


class TestExtractPendingTool:
    def test_finds_pending_tool(self):
        entries = _load_fixture("running_tool_pending.jsonl")
        result = extract_pending_tool(entries)
        assert len(result) == 1
        name, input_data = result[0]
        assert name == "Bash"
        assert "pytest" in input_data.get("command", "")

    def test_no_pending_when_all_resolved(self):
        entries = _load_fixture("sample_session.jsonl")
        result = extract_pending_tool(entries)
        assert result == []

    def test_no_pending_on_empty(self):
        result = extract_pending_tool([])
        assert result == []

    def test_returns_multiple_pending_tools(self):
        """CC can batch multiple tool_use blocks; all pending should be returned."""
        entries = [
            {
                "type": "assistant",
                "message": {
                    "role": "assistant",
                    "content": [
                        {"type": "tool_use", "id": "t1", "name": "Read", "input": {"file_path": "/a.py"}},
                        {"type": "tool_use", "id": "t2", "name": "Read", "input": {"file_path": "/b.py"}},
                    ],
                },
                "timestamp": "2026-03-12T10:00:00.000Z",
            }
        ]
        result = extract_pending_tool(entries)
        assert len(result) == 2
        names = [name for name, _ in result]
        assert names == ["Read", "Read"]


# --- extract_session_metadata ---


class TestExtractSessionMetadata:
    def test_extracts_metadata(self):
        entries = _load_fixture("sample_session.jsonl")
        meta = extract_session_metadata(entries)
        assert meta["sessionId"] == "sess-001"
        assert meta["slug"] == "happy-coding-fox"
        assert meta["cwd"] == "/Users/test/repos/myproject"
        assert meta["gitBranch"] == "main"

    def test_partial_metadata(self):
        entries = [
            {
                "type": "user",
                "sessionId": "only-session",
                "timestamp": "2026-03-12T10:00:00.000Z",
            },
        ]
        meta = extract_session_metadata(entries)
        assert meta["sessionId"] == "only-session"
        assert "slug" not in meta

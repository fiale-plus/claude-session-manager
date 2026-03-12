"""Tests for csm.scanner — path encoding, session building, sort order."""

from __future__ import annotations

from pathlib import Path

from csm.models import SessionState
from csm.scanner import _session_from_jsonl, encode_project_path

FIXTURES = Path(__file__).parent / "fixtures"


class TestEncodeProjectPath:
    def test_basic_path(self):
        assert encode_project_path("/Users/pavel/repos/foo") == "-Users-pavel-repos-foo"

    def test_nested_path(self):
        result = encode_project_path("/Users/pavel/repos/fiale-plus/claude-session-manager")
        assert result == "-Users-pavel-repos-fiale-plus-claude-session-manager"

    def test_trailing_slash_stripped(self):
        assert encode_project_path("/Users/pavel/repos/foo/") == "-Users-pavel-repos-foo"

    def test_root_path(self):
        assert encode_project_path("/") == "-"

    def test_home_path(self):
        assert encode_project_path("/Users/pavel") == "-Users-pavel"


class TestSessionFromJsonl:
    def test_builds_session_from_sample(self):
        session = _session_from_jsonl(FIXTURES / "sample_session.jsonl", pid=1234)
        assert session is not None
        assert session.session_id == "sess-001"
        assert session.slug == "happy-coding-fox"
        assert session.project_path == "/Users/test/repos/myproject"
        assert session.git_branch == "main"
        assert session.pid == 1234
        assert session.state == SessionState.WAITING
        assert len(session.activities) > 0
        assert session.last_text != ""

    def test_dead_state_when_no_pid(self):
        session = _session_from_jsonl(FIXTURES / "sample_session.jsonl", pid=None)
        assert session is not None
        assert session.state == SessionState.DEAD

    def test_running_state_with_pending_tool(self):
        session = _session_from_jsonl(FIXTURES / "running_tool_pending.jsonl", pid=9999)
        assert session is not None
        assert session.state == SessionState.RUNNING
        assert session.session_id == "sess-002"

    def test_returns_none_for_nonexistent_file(self):
        session = _session_from_jsonl(Path("/nonexistent/path.jsonl"), pid=1)
        assert session is None


class TestSessionSortOrder:
    """Verify the sort order: RUNNING < WAITING < IDLE < DEAD."""

    def test_running_before_waiting_before_dead(self):
        running = _session_from_jsonl(FIXTURES / "running_tool_pending.jsonl", pid=100)
        waiting = _session_from_jsonl(FIXTURES / "sample_session.jsonl", pid=200)
        dead = _session_from_jsonl(FIXTURES / "sample_session.jsonl", pid=None)

        assert running is not None
        assert waiting is not None
        assert dead is not None
        assert running.state == SessionState.RUNNING
        assert waiting.state == SessionState.WAITING
        assert dead.state == SessionState.DEAD

        # Apply the same sort key used in discover_sessions
        state_order = {
            SessionState.RUNNING: 0,
            SessionState.WAITING: 1,
            SessionState.IDLE: 2,
            SessionState.DEAD: 3,
        }
        sessions = [dead, waiting, running]  # deliberately wrong order
        sessions.sort(
            key=lambda s: (
                state_order.get(s.state, 9),
                -(s.last_activity_time.timestamp() if s.last_activity_time else 0),
            )
        )
        assert sessions[0].state == SessionState.RUNNING
        assert sessions[1].state == SessionState.WAITING
        assert sessions[2].state == SessionState.DEAD

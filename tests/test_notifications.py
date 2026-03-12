"""Tests for csm.notifications — macOS desktop notifications."""

from __future__ import annotations

import time
from unittest.mock import MagicMock, patch

import pytest

from csm.models import Session, SessionState
from csm.notifications import SessionNotifier, notify


# ── notify() ──────────────────────────────────────────────────────


class TestNotifyFunction:
    @patch("csm.notifications.subprocess.run")
    def test_calls_osascript_with_sound(self, mock_run):
        notify("Test Title", "Test Message", sound=True)
        mock_run.assert_called_once()
        args = mock_run.call_args
        cmd = args[0][0]
        assert cmd[0] == "osascript"
        assert cmd[1] == "-e"
        script = cmd[2]
        assert 'display notification "Test Message"' in script
        assert 'with title "Test Title"' in script
        assert 'sound name "default"' in script

    @patch("csm.notifications.subprocess.run")
    def test_calls_osascript_without_sound(self, mock_run):
        notify("Title", "Msg", sound=False)
        mock_run.assert_called_once()
        script = mock_run.call_args[0][0][2]
        assert "sound name" not in script

    @patch("csm.notifications.subprocess.run")
    def test_escapes_quotes_in_title_and_message(self, mock_run):
        notify('Say "hello"', 'It\'s a "test"')
        mock_run.assert_called_once()
        script = mock_run.call_args[0][0][2]
        # Double-quotes should be escaped
        assert '\\"hello\\"' in script
        assert '\\"test\\"' in script

    @patch("csm.notifications.subprocess.run", side_effect=FileNotFoundError)
    def test_handles_missing_osascript_gracefully(self, mock_run):
        # Should not raise
        notify("Title", "Message")

    @patch("csm.notifications.subprocess.run", side_effect=OSError("fail"))
    def test_handles_os_error_gracefully(self, mock_run):
        # Should not raise
        notify("Title", "Message")


# ── SessionNotifier ───────────────────────────────────────────────


def _make_session(
    *,
    session_id: str = "sess-1",
    state: SessionState = SessionState.RUNNING,
    autopilot: bool = False,
    has_destructive_pending: bool = False,
    project_name: str = "myproject",
) -> Session:
    """Helper to create a minimal Session for testing."""
    from pathlib import Path

    return Session(
        session_id=session_id,
        slug="",
        project_path="/tmp/test",
        project_name=project_name,
        jsonl_path=Path("/tmp/test.jsonl"),
        state=state,
        autopilot=autopilot,
        has_destructive_pending=has_destructive_pending,
    )


class TestSessionNotifier:
    @patch("csm.notifications.notify")
    def test_notifies_on_running_to_waiting(self, mock_notify):
        notifier = SessionNotifier()

        # First poll: RUNNING
        sessions = [_make_session(state=SessionState.RUNNING)]
        notifier.check(sessions)
        mock_notify.assert_not_called()

        # Second poll: WAITING
        sessions = [_make_session(state=SessionState.WAITING)]
        notifier.check(sessions)
        mock_notify.assert_called_once()
        title, message = mock_notify.call_args[0]
        assert "needs input" in title.lower() or "needs input" in message.lower()

    @patch("csm.notifications.notify")
    def test_no_notification_on_same_state(self, mock_notify):
        notifier = SessionNotifier()

        sessions = [_make_session(state=SessionState.RUNNING)]
        notifier.check(sessions)
        notifier.check(sessions)
        notifier.check(sessions)
        mock_notify.assert_not_called()

    @patch("csm.notifications.notify")
    def test_no_notification_waiting_to_running(self, mock_notify):
        """Only RUNNING->WAITING fires, not the reverse."""
        notifier = SessionNotifier()

        notifier.check([_make_session(state=SessionState.WAITING)])
        notifier.check([_make_session(state=SessionState.RUNNING)])
        mock_notify.assert_not_called()

    @patch("csm.notifications.notify")
    def test_notifies_on_destructive_pending(self, mock_notify):
        notifier = SessionNotifier()

        # First poll: autopilot on, no destructive
        sessions = [_make_session(autopilot=True, has_destructive_pending=False)]
        notifier.check(sessions)
        mock_notify.assert_not_called()

        # Second poll: destructive pending appears
        sessions = [_make_session(autopilot=True, has_destructive_pending=True)]
        notifier.check(sessions)
        mock_notify.assert_called_once()
        title, message = mock_notify.call_args[0]
        assert "approval" in title.lower() or "approval" in message.lower()

    @patch("csm.notifications.notify")
    def test_no_destructive_notification_without_autopilot(self, mock_notify):
        """Destructive pending only fires notification when autopilot is on."""
        notifier = SessionNotifier()

        notifier.check([_make_session(autopilot=False, has_destructive_pending=False)])
        notifier.check([_make_session(autopilot=False, has_destructive_pending=True)])
        mock_notify.assert_not_called()

    @patch("csm.notifications.notify")
    def test_rate_limiting(self, mock_notify):
        notifier = SessionNotifier(rate_limit=1000.0)  # Very long rate limit

        # First notification should go through
        notifier.check([_make_session(state=SessionState.RUNNING)])
        notifier.check([_make_session(state=SessionState.WAITING)])
        assert mock_notify.call_count == 1

        # Try again — should be rate-limited
        notifier.check([_make_session(state=SessionState.RUNNING)])
        notifier.check([_make_session(state=SessionState.WAITING)])
        assert mock_notify.call_count == 1  # Still 1

    @patch("csm.notifications.notify")
    def test_rate_limit_expires(self, mock_notify):
        notifier = SessionNotifier(rate_limit=0.0)  # No rate limit

        notifier.check([_make_session(state=SessionState.RUNNING)])
        notifier.check([_make_session(state=SessionState.WAITING)])
        assert mock_notify.call_count == 1

        notifier.check([_make_session(state=SessionState.RUNNING)])
        notifier.check([_make_session(state=SessionState.WAITING)])
        assert mock_notify.call_count == 2

    @patch("csm.notifications.notify")
    def test_multiple_sessions_independent(self, mock_notify):
        notifier = SessionNotifier(rate_limit=0.0)

        sessions = [
            _make_session(session_id="s1", state=SessionState.RUNNING, project_name="proj1"),
            _make_session(session_id="s2", state=SessionState.WAITING, project_name="proj2"),
        ]
        notifier.check(sessions)
        mock_notify.assert_not_called()  # No previous state to transition from

        # Now s1 goes to WAITING
        sessions = [
            _make_session(session_id="s1", state=SessionState.WAITING, project_name="proj1"),
            _make_session(session_id="s2", state=SessionState.WAITING, project_name="proj2"),
        ]
        notifier.check(sessions)
        assert mock_notify.call_count == 1  # Only s1 transitioned

    @patch("csm.notifications.notify")
    def test_forget_clears_session_state(self, mock_notify):
        notifier = SessionNotifier(rate_limit=0.0)

        notifier.check([_make_session(state=SessionState.RUNNING)])
        notifier.forget("sess-1")

        # After forget, transition shouldn't fire (no previous state)
        notifier.check([_make_session(state=SessionState.WAITING)])
        mock_notify.assert_not_called()

    @patch("csm.notifications.notify")
    def test_clear_resets_everything(self, mock_notify):
        notifier = SessionNotifier(rate_limit=0.0)

        notifier.check([_make_session(state=SessionState.RUNNING)])
        notifier.clear()

        # After clear, no previous state — no transition notification
        notifier.check([_make_session(state=SessionState.WAITING)])
        mock_notify.assert_not_called()


class TestSelfExclusion:
    """Tests for the _is_self_session helper in app.py."""

    def test_matches_own_cwd(self):
        from csm.app import _is_self_session

        session = _make_session()
        session.project_path = "/Users/pavel/repos/myproject"
        assert _is_self_session(session, "/Users/pavel/repos/myproject") is True

    def test_trailing_slash_normalized(self):
        from csm.app import _is_self_session

        session = _make_session()
        session.project_path = "/Users/pavel/repos/myproject/"
        assert _is_self_session(session, "/Users/pavel/repos/myproject") is True

    def test_different_paths_no_match(self):
        from csm.app import _is_self_session

        session = _make_session()
        session.project_path = "/Users/pavel/repos/other"
        assert _is_self_session(session, "/Users/pavel/repos/myproject") is False

    def test_empty_paths(self):
        from csm.app import _is_self_session

        session = _make_session()
        session.project_path = ""
        assert _is_self_session(session, "/Users/pavel") is False

        session.project_path = "/Users/pavel"
        assert _is_self_session(session, "") is False

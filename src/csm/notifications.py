"""macOS desktop notifications for session state transitions.

Uses ``osascript`` to fire native Notification Centre alerts when:
- A session transitions from RUNNING to WAITING (CC finished, needs input).
- Autopilot encounters a destructive command that needs manual approval.

Rate-limited: at most one notification per session per 30 seconds.
"""

from __future__ import annotations

import logging
import subprocess
import time

from csm.models import Session, SessionState

log = logging.getLogger(__name__)

# Minimum seconds between notifications for the same session.
RATE_LIMIT_SECS = 30.0


def notify(title: str, message: str, *, sound: bool = True) -> None:
    """Send a macOS notification via osascript.

    Silently drops errors (non-macOS, osascript missing, etc.).
    """
    # Escape double-quotes and backslashes for AppleScript string literals.
    safe_title = title.replace("\\", "\\\\").replace('"', '\\"')
    safe_message = message.replace("\\", "\\\\").replace('"', '\\"')

    sound_clause = ' sound name "default"' if sound else ""
    script = (
        f'display notification "{safe_message}" '
        f'with title "{safe_title}"{sound_clause}'
    )

    try:
        subprocess.run(
            ["osascript", "-e", script],
            capture_output=True,
            timeout=5,
        )
    except (FileNotFoundError, subprocess.TimeoutExpired, OSError):
        log.debug("Failed to send notification", exc_info=True)


class SessionNotifier:
    """Tracks per-session state and fires notifications on interesting transitions.

    Call ``check(sessions)`` each poll cycle.  The notifier remembers the
    previous state and last-notification timestamp per session, so duplicate
    alerts are suppressed.
    """

    def __init__(self, rate_limit: float = RATE_LIMIT_SECS) -> None:
        # session_id -> previous SessionState
        self._prev_state: dict[str, SessionState] = {}
        # session_id -> previous has_destructive_pending flag
        self._prev_destructive: dict[str, bool] = {}
        # session_id -> monotonic timestamp of last notification sent
        self._last_notified: dict[str, float] = {}
        self._rate_limit = rate_limit

    def check(self, sessions: list[Session]) -> None:
        """Inspect sessions and fire notifications for state transitions."""
        now = time.monotonic()

        for session in sessions:
            sid = session.session_id
            prev = self._prev_state.get(sid)
            name = session.project_name or session.slug or sid

            # Transition: RUNNING -> WAITING  (CC finished, needs input)
            if prev == SessionState.RUNNING and session.state == SessionState.WAITING:
                self._maybe_notify(
                    sid,
                    now,
                    title="Session needs input",
                    message=f"{name}: Claude finished — waiting for you",
                )

            # Autopilot blocked by destructive tool
            prev_destr = self._prev_destructive.get(sid, False)
            if (
                session.autopilot
                and session.has_destructive_pending
                and not prev_destr
            ):
                self._maybe_notify(
                    sid,
                    now,
                    title="Manual approval needed",
                    message=f"{name}: destructive tool call needs your approval",
                )

            # Update tracked state
            self._prev_state[sid] = session.state
            self._prev_destructive[sid] = session.has_destructive_pending

    def _maybe_notify(
        self, session_id: str, now: float, *, title: str, message: str
    ) -> None:
        """Send notification if rate limit allows."""
        last = self._last_notified.get(session_id, 0.0)
        if now - last < self._rate_limit:
            log.debug(
                "Notification rate-limited for session %s", session_id
            )
            return
        notify(title, message)
        self._last_notified[session_id] = now

    def forget(self, session_id: str) -> None:
        """Drop tracking state for a session."""
        self._prev_state.pop(session_id, None)
        self._prev_destructive.pop(session_id, None)
        self._last_notified.pop(session_id, None)

    def clear(self) -> None:
        """Reset all tracking state."""
        self._prev_state.clear()
        self._prev_destructive.clear()
        self._last_notified.clear()

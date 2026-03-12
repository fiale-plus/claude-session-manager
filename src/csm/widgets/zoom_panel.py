"""Expanded preview panel for a selected session."""

from __future__ import annotations

from pathlib import Path

from textual.containers import Vertical
from textual.widgets import Static

from csm.models import Session, SessionState
from csm.widgets.activity_log import ActivityLog
from csm.widgets.session_pill import STATE_ICONS, _elapsed

MAX_MOTIVATION_LINES = 4
MAX_MOTIVATION_CHARS = 300


class ZoomPanel(Vertical):
    """Zoom preview shown above the session strip when navigating."""

    DEFAULT_CSS = ""

    def __init__(self, **kwargs) -> None:
        super().__init__(**kwargs)
        self._session: Session | None = None

    def compose(self):
        yield Static(id="zoom-header")
        yield ActivityLog(id="zoom-activity")
        yield Static(id="zoom-motivation")

    def update_session(self, session: Session | None) -> None:
        """Populate panel with session data."""
        self._session = session

        header = self.query_one("#zoom-header", Static)
        activity_log = self.query_one("#zoom-activity", ActivityLog)
        motivation = self.query_one("#zoom-motivation", Static)

        if session is None:
            header.update("")
            activity_log.update_activities([])
            motivation.update("")
            return

        # -- Header --
        state_icon = STATE_ICONS.get(session.state, "?")
        indicator = "\u25CF" if session.state != SessionState.DEAD else "\u25CB"  # ● or ○
        elapsed = _elapsed(session.last_activity_time)
        name = session.project_name or session.slug or "?"
        path_display = session.project_path
        # Shorten home prefix
        home = str(Path.home())
        if path_display.startswith(home):
            path_display = "~" + path_display[len(home):]
        branch = f"  ({session.git_branch})" if session.git_branch else ""
        header.update(
            f" {indicator} {name}    {state_icon} {elapsed}    {path_display}{branch}"
        )

        # -- Activity timeline --
        activity_log.update_activities(session.activities[-8:])

        # -- Motivation text --
        text = session.last_text.strip()
        if text:
            # Truncate to MAX_MOTIVATION_CHARS then limit lines
            text = text[:MAX_MOTIVATION_CHARS]
            lines = text.splitlines()[:MAX_MOTIVATION_LINES]
            text = "\n".join(lines)
            motivation.update(f"  {text}")
        else:
            motivation.update("  (no recent text)")

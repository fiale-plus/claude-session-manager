"""Single session badge widget."""

from __future__ import annotations

from datetime import datetime

from textual.widget import Widget
from textual.reactive import reactive

from csm.models import Session, SessionState

STATE_ICONS: dict[SessionState, str] = {
    SessionState.RUNNING: "\u2699",   # ⚙
    SessionState.WAITING: "\u23F3",   # ⏳
    SessionState.IDLE: "\u2713",      # ✓
    SessionState.DEAD: "\u25CB",      # ○
}

STATE_CSS_CLASS: dict[SessionState, str] = {
    SessionState.RUNNING: "running",
    SessionState.WAITING: "waiting",
    SessionState.IDLE: "idle",
    SessionState.DEAD: "dead",
}


def _elapsed(dt: datetime | None) -> str:
    """Human-readable elapsed time since *dt*."""
    if dt is None:
        return "?"
    delta = datetime.now(tz=dt.tzinfo) - dt
    secs = int(delta.total_seconds())
    if secs < 0:
        return "0s"
    if secs < 60:
        return f"{secs}s"
    mins = secs // 60
    if mins < 60:
        return f"{mins}m"
    hours = mins // 60
    if hours < 24:
        return f"{hours}h"
    days = hours // 24
    return f"{days}d"


class SessionPill(Widget):
    """Compact badge showing one session's status."""

    selected: reactive[bool] = reactive(False)

    def __init__(self, session: Session, **kwargs) -> None:
        super().__init__(**kwargs)
        self.session = session

    def render(self) -> str:
        icon = STATE_ICONS.get(self.session.state, "?")
        elapsed = _elapsed(self.session.last_activity_time)
        name = self.session.project_name or self.session.slug or "?"
        ap = " \u25C9" if self.session.autopilot else ""
        return f" {icon} {name} {elapsed}{ap} "

    def watch_selected(self, value: bool) -> None:
        if value:
            self.add_class("selected")
        else:
            self.remove_class("selected")

    def update_session(self, session: Session) -> None:
        """Update the underlying session data and refresh display."""
        old_state_class = STATE_CSS_CLASS.get(self.session.state)
        self.session = session
        new_state_class = STATE_CSS_CLASS.get(session.state)
        if old_state_class and old_state_class != new_state_class:
            self.remove_class(old_state_class)
        if new_state_class:
            self.add_class(new_state_class)
        self._sync_autopilot_classes()
        self.refresh()

    def _sync_autopilot_classes(self) -> None:
        """Toggle .autopilot-on and .autopilot-warning CSS classes."""
        if self.session.autopilot:
            self.add_class("autopilot-on")
            if self.session.has_destructive_pending:
                self.add_class("autopilot-warning")
            else:
                self.remove_class("autopilot-warning")
        else:
            self.remove_class("autopilot-on")
            self.remove_class("autopilot-warning")

    def on_mount(self) -> None:
        state_class = STATE_CSS_CLASS.get(self.session.state)
        if state_class:
            self.add_class(state_class)
        self._sync_autopilot_classes()

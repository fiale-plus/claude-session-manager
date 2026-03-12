"""Single session badge widget."""

from __future__ import annotations

from datetime import datetime

from textual.timer import Timer
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

# Pulse interval (seconds) — how often the dim class is toggled.
# autopilot-on: slow pulse (1s on, 1s off = 2s cycle)
# autopilot-warning: faster pulse (0.5s on, 0.5s off = 1s cycle)
_PULSE_SLOW_INTERVAL = 1.0
_PULSE_FAST_INTERVAL = 0.5


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
        self._pulse_timer: Timer | None = None
        self._pulse_dim = False

    def render(self) -> str:
        icon = STATE_ICONS.get(self.session.state, "?")
        elapsed = _elapsed(self.session.last_activity_time)
        name = self.session.project_name or self.session.slug or "?"
        self_tag = " [self]" if self.session.is_self else ""
        ap = " \u25C9" if self.session.autopilot else ""
        return f" {icon} {name}{self_tag} {elapsed}{ap} "

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
        self._sync_pulse_timer()
        self.refresh()

    def _sync_autopilot_classes(self) -> None:
        """Toggle .autopilot-on, .autopilot-warning, and .self-session CSS classes."""
        if self.session.autopilot:
            self.add_class("autopilot-on")
            if self.session.has_destructive_pending:
                self.add_class("autopilot-warning")
            else:
                self.remove_class("autopilot-warning")
        else:
            self.remove_class("autopilot-on")
            self.remove_class("autopilot-warning")

        if self.session.is_self:
            self.add_class("self-session")
        else:
            self.remove_class("self-session")

    def _sync_pulse_timer(self) -> None:
        """Start, stop, or adjust the pulse timer based on autopilot state."""
        needs_pulse = self.session.autopilot
        if self.session.has_destructive_pending:
            interval = _PULSE_FAST_INTERVAL
        else:
            interval = _PULSE_SLOW_INTERVAL

        if needs_pulse:
            if self._pulse_timer is None:
                self._pulse_timer = self.set_interval(
                    interval, self._toggle_pulse, pause=False
                )
            else:
                # Timer already running — no need to recreate for interval
                # change every update; the visual difference is minor.
                pass
        else:
            self._stop_pulse()

    def _toggle_pulse(self) -> None:
        """Toggle the dim class for the pulse effect."""
        self._pulse_dim = not self._pulse_dim
        if self._pulse_dim:
            self.add_class("autopilot-pulse-dim")
        else:
            self.remove_class("autopilot-pulse-dim")

    def _stop_pulse(self) -> None:
        """Stop pulse timer and remove dim class."""
        if self._pulse_timer is not None:
            self._pulse_timer.stop()
            self._pulse_timer = None
        self._pulse_dim = False
        self.remove_class("autopilot-pulse-dim")

    def on_mount(self) -> None:
        state_class = STATE_CSS_CLASS.get(self.session.state)
        if state_class:
            self.add_class(state_class)
        self._sync_autopilot_classes()
        self._sync_pulse_timer()

    def on_unmount(self) -> None:
        """Clean up pulse timer on removal."""
        self._stop_pulse()

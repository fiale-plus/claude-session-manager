"""Horizontal strip container for session pills."""

from __future__ import annotations

from textual.containers import HorizontalScroll
from textual.reactive import reactive

from csm.models import Session
from csm.widgets.session_pill import SessionPill


class SessionStrip(HorizontalScroll):
    """Horizontally-scrollable row of SessionPill widgets, docked to bottom."""

    DEFAULT_CSS = """
    SessionStrip {
        dock: bottom;
        height: 3;
        min-height: 3;
        max-height: 3;
    }
    """

    can_focus = False

    selected_index: reactive[int] = reactive(0)

    def __init__(self, **kwargs) -> None:
        super().__init__(**kwargs)
        self._pills: list[SessionPill] = []

    def update_sessions(self, sessions: list[Session]) -> None:
        """Reconcile the pill list with an updated session list."""
        existing_ids = {p.session.session_id: p for p in self._pills}
        new_ids = {s.session_id for s in sessions}

        # Remove pills for sessions that no longer exist
        for sid, pill in list(existing_ids.items()):
            if sid not in new_ids:
                pill.remove()
                self._pills.remove(pill)

        # Update existing or add new
        updated_pills: list[SessionPill] = []
        for session in sessions:
            if session.session_id in existing_ids:
                pill = existing_ids[session.session_id]
                pill.update_session(session)
                updated_pills.append(pill)
            else:
                pill = SessionPill(session)
                self.mount(pill)
                updated_pills.append(pill)

        self._pills = updated_pills

        # Clamp selected index
        if self._pills:
            self.selected_index = min(self.selected_index, len(self._pills) - 1)
        else:
            self.selected_index = 0

        self._apply_selection()

    def watch_selected_index(self, value: int) -> None:
        self._apply_selection()

    def _apply_selection(self) -> None:
        """Highlight only the selected pill."""
        for i, pill in enumerate(self._pills):
            pill.selected = i == self.selected_index
        # Scroll the selected pill into view
        if self._pills and 0 <= self.selected_index < len(self._pills):
            self._pills[self.selected_index].scroll_visible()

    def select_next(self) -> None:
        if self._pills:
            self.selected_index = (self.selected_index + 1) % len(self._pills)

    def select_prev(self) -> None:
        if self._pills:
            self.selected_index = (self.selected_index - 1) % len(self._pills)

    @property
    def selected_session(self) -> Session | None:
        if self._pills and 0 <= self.selected_index < len(self._pills):
            return self._pills[self.selected_index].session
        return None

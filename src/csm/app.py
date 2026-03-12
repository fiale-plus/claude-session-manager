"""Main Textual TUI application for Claude Session Manager."""

from __future__ import annotations

import random
from pathlib import Path

from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.reactive import reactive
from textual.widgets import Static

from csm import ghostty
from csm.scanner import discover_sessions
from csm.widgets.session_strip import SessionStrip
from csm.widgets.zoom_panel import ZoomPanel

HINTS = [
    "Arrow keys: navigate sessions",
    "'q' to quit",
    "Enter: switch to Ghostty tab",
    "Escape: collapse zoom panel",
]

# TODO Phase 4 hints — uncomment as features land:
# "Press 'a' to toggle autopilot",
# "Press 'Q' for approval queue",
# "Press 'y' to approve pending tool call",
# "Press 'n' to reject pending tool call",

HINT_ROTATE_SECS = 45


class SessionManagerApp(App):
    """Compact TUI for monitoring Claude Code sessions."""

    CSS_PATH = Path("css/app.tcss")
    TITLE = "csm"

    BINDINGS = [
        Binding("left", "select_prev", "Prev session", show=False, priority=True),
        Binding("right", "select_next", "Next session", show=False, priority=True),
        Binding("q", "quit", "Quit"),
        Binding("enter", "switch_tab", "Switch tab", show=False),
        Binding("escape", "collapse_zoom", "Collapse", show=False),
        # Placeholder bindings — Phase 4 will implement these
        Binding("a", "noop", "Autopilot", show=False),
        Binding("shift+q", "noop", "Queue", show=False),
        Binding("y", "noop", "Approve", show=False),
        Binding("n", "noop", "Reject", show=False),
    ]

    zoomed: reactive[bool] = reactive(False)

    def compose(self) -> ComposeResult:
        yield Static(id="main-area")
        yield ZoomPanel(id="zoom-panel")
        yield Static(random.choice(HINTS), id="hints-bar")
        yield SessionStrip()

    def on_mount(self) -> None:
        self.poll_sessions()
        self.set_interval(2, self.poll_sessions)
        self.set_interval(HINT_ROTATE_SECS, self._rotate_hint)

    def poll_sessions(self) -> None:
        """Discover sessions off the main thread, then update the strip."""
        self.run_worker(self._poll_sessions_worker, thread=True)

    def _poll_sessions_worker(self) -> None:
        sessions = discover_sessions()
        strip = self.query_one(SessionStrip)
        self.call_from_thread(strip.update_sessions, sessions)
        # If zoomed, refresh the zoom panel with the currently selected session
        if self.zoomed:
            self.call_from_thread(self._refresh_zoom)

    def _rotate_hint(self) -> None:
        hints_bar = self.query_one("#hints-bar", Static)
        hints_bar.update(random.choice(HINTS))

    def watch_zoomed(self, value: bool) -> None:
        """Toggle zoom panel visibility."""
        panel = self.query_one("#zoom-panel", ZoomPanel)
        if value:
            panel.add_class("visible")
            self._refresh_zoom()
        else:
            panel.remove_class("visible")

    def _refresh_zoom(self) -> None:
        """Update the zoom panel with the currently selected session."""
        strip = self.query_one(SessionStrip)
        panel = self.query_one("#zoom-panel", ZoomPanel)
        panel.update_session(strip.selected_session)

    # ── Actions ──────────────────────────────────────────────

    def action_select_next(self) -> None:
        self.query_one(SessionStrip).select_next()
        if self.zoomed:
            self._refresh_zoom()
        else:
            self.zoomed = True

    def action_select_prev(self) -> None:
        self.query_one(SessionStrip).select_prev()
        if self.zoomed:
            self._refresh_zoom()
        else:
            self.zoomed = True

    def action_switch_tab(self) -> None:
        """Switch to the Ghostty tab for the selected session."""
        strip = self.query_one(SessionStrip)
        session = strip.selected_session
        if session is None:
            return
        if session.ghostty_tab_name:
            self.run_worker(
                lambda: ghostty.switch_to_tab(session.ghostty_tab_name),
                thread=True,
            )
        else:
            hints_bar = self.query_one("#hints-bar", Static)
            hints_bar.update("No Ghostty tab found for this session")

    def action_collapse_zoom(self) -> None:
        """Collapse the zoom panel."""
        self.zoomed = False

    def action_noop(self) -> None:
        """Placeholder for future-phase key bindings."""

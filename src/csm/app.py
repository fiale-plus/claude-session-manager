"""Main Textual TUI application for Claude Session Manager."""

from __future__ import annotations

import random
from pathlib import Path

from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.widgets import Static

from csm.scanner import discover_sessions
from csm.widgets.session_strip import SessionStrip

HINTS = [
    "Arrow keys: navigate sessions",
    "'q' to quit",
]

# TODO Phase 3/4 hints — uncomment as features land:
# "Press Enter to zoom into session",
# "Press Escape to collapse detail",
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
        # Placeholder bindings — Phase 3/4 will implement these
        Binding("enter", "noop", "Zoom", show=False),
        Binding("escape", "noop", "Collapse", show=False),
        Binding("a", "noop", "Autopilot", show=False),
        Binding("shift+q", "noop", "Queue", show=False),
        Binding("y", "noop", "Approve", show=False),
        Binding("n", "noop", "Reject", show=False),
    ]

    def compose(self) -> ComposeResult:
        yield Static(id="main-area")
        yield Static(random.choice(HINTS), id="hints-bar")
        yield SessionStrip()

    def on_mount(self) -> None:
        self.poll_sessions()
        self.set_interval(2, self.poll_sessions)
        self.set_interval(HINT_ROTATE_SECS, self._rotate_hint)

    def poll_sessions(self) -> None:
        """Discover sessions off the main thread, then update the strip."""
        self.run_worker(self._poll_sessions_worker, thread=True)

    async def _poll_sessions_worker(self) -> None:
        sessions = discover_sessions()
        strip = self.query_one(SessionStrip)
        self.call_from_thread(strip.update_sessions, sessions)

    def _rotate_hint(self) -> None:
        hints_bar = self.query_one("#hints-bar", Static)
        hints_bar.update(random.choice(HINTS))

    # ── Actions ──────────────────────────────────────────────

    def action_select_next(self) -> None:
        self.query_one(SessionStrip).select_next()

    def action_select_prev(self) -> None:
        self.query_one(SessionStrip).select_prev()

    def action_noop(self) -> None:
        """Placeholder for future-phase key bindings."""

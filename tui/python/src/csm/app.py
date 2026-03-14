"""Main Textual TUI application for Claude Session Manager.

Thin client: all session discovery, autopilot logic, and Ghostty keystroke
management live in the Go daemon.  This TUI subscribes to session updates
over a Unix socket and sends approve/reject/toggle commands back.
"""

from __future__ import annotations

import asyncio
import logging
import random
from pathlib import Path

from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.reactive import reactive
from textual.widgets import Static

from csm.client import DaemonClient
from csm.models import Session
from csm.widgets.approval_queue import ApprovalQueue
from csm.widgets.session_strip import SessionStrip
from csm.widgets.zoom_panel import ZoomPanel

log = logging.getLogger(__name__)

HINTS = [
    "Arrow keys: navigate sessions",
    "'q' to quit",
    "Enter: focus (switch to) session's Ghostty tab",
    "Escape: collapse zoom panel",
    "Press 'a' to toggle autopilot for the selected session",
    "Press 'Q' for the approval queue overlay",
    "Press 'y' to approve the pending tool call",
    "Press 'n' to reject the pending tool call",
    "Press 'A' to approve all safe tool calls at once",
    "Press 'h' for help",
    "Autopilot auto-approves safe tools, pauses on destructive ones",
    "Destructive commands (git push, rm, etc.) always need manual approval",
]

HINT_ROTATE_SECS = 45


class SessionManagerApp(App):
    """Compact TUI for monitoring Claude Code sessions."""

    CSS_PATH = Path("css/app.tcss")
    TITLE = "csm"

    BINDINGS = [
        Binding("left", "select_prev", "Prev session", show=False, priority=True),
        Binding("right", "select_next", "Next session", show=False, priority=True),
        Binding("q", "quit", "Quit"),
        Binding("enter", "focus_session", "Focus session", show=False),
        Binding("escape", "collapse", "Collapse", show=False),
        Binding("a", "toggle_autopilot", "Autopilot", show=False),
        Binding("h", "toggle_help", "Help", show=False),
        Binding("Q", "toggle_queue", "Queue", show=False),
        Binding("y", "approve", "Approve", show=False),
        Binding("n", "reject", "Reject", show=False),
        Binding("A", "approve_all_safe", "Approve all safe", show=False),
        Binding("up", "queue_up", "Queue up", show=False, priority=True),
        Binding("down", "queue_down", "Queue down", show=False, priority=True),
    ]

    zoomed: reactive[bool] = reactive(False)
    queue_visible: reactive[bool] = reactive(False)

    def __init__(self, **kwargs) -> None:
        super().__init__(**kwargs)
        self._client = DaemonClient()
        self._sessions: list[Session] = []
        self._subscription_task: asyncio.Task | None = None

    def compose(self) -> ComposeResult:
        yield Static(id="main-area")
        yield ZoomPanel(id="zoom-panel")
        yield ApprovalQueue(id="approval-queue")
        yield Static(random.choice(HINTS), id="hints-bar")
        yield SessionStrip()

    async def on_mount(self) -> None:
        self.set_interval(HINT_ROTATE_SECS, self._rotate_hint)
        self._subscription_task = asyncio.create_task(self._subscription_worker())

    async def _subscription_worker(self) -> None:
        """Connect to the daemon and stream session updates."""
        while True:
            try:
                await self._client.connect()
                self._hint("Connected to daemon")
                async for sessions in self._client.subscribe():
                    self._sessions = sessions
                    self._update_ui(sessions)
            except (ConnectionError, OSError) as exc:
                log.debug("Daemon connection lost: %s", exc)
                self._hint("Daemon disconnected — reconnecting...")
                self._sessions = []
                self._update_ui([])
            except asyncio.CancelledError:
                return
            except Exception:
                log.exception("Subscription worker error")

            # Back off before reconnecting.
            try:
                await asyncio.sleep(2)
            except asyncio.CancelledError:
                return

    def _update_ui(self, sessions: list[Session]) -> None:
        """Push a session list into all visible widgets."""
        if not sessions:
            self._show_empty_state()
            return

        strip = self.query_one(SessionStrip)
        strip.update_sessions(sessions)

        if self.zoomed:
            self._refresh_zoom()

        if self.queue_visible:
            queue = self.query_one("#approval-queue", ApprovalQueue)
            queue.update_queue(sessions)

    def _show_empty_state(self) -> None:
        """Update UI when no sessions are discovered."""
        strip = self.query_one(SessionStrip)
        strip.update_sessions([])
        self._hint("No Claude sessions found. Start a Claude Code session to begin.")

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

    def watch_queue_visible(self, value: bool) -> None:
        """Toggle approval queue visibility."""
        queue = self.query_one("#approval-queue", ApprovalQueue)
        if value:
            queue.add_class("visible")
            queue.update_queue(self._sessions)
        else:
            queue.remove_class("visible")

    def _refresh_zoom(self) -> None:
        """Update the zoom panel with the currently selected session."""
        strip = self.query_one(SessionStrip)
        panel = self.query_one("#zoom-panel", ZoomPanel)
        panel.update_session(strip.selected_session)

    def _hint(self, text: str) -> None:
        """Show a transient hint in the hints bar."""
        self.query_one("#hints-bar", Static).update(text)

    # -- Graceful shutdown --------------------------------------------------

    async def action_quit(self) -> None:
        """Cancel subscription and disconnect from daemon, then quit."""
        if self._subscription_task is not None:
            self._subscription_task.cancel()
            try:
                await self._subscription_task
            except asyncio.CancelledError:
                pass
        await self._client.disconnect()
        await super().action_quit()

    # -- Actions ------------------------------------------------------------

    def action_select_next(self) -> None:
        if self.queue_visible:
            self.query_one("#approval-queue", ApprovalQueue).move_down()
            return
        strip = self.query_one(SessionStrip)
        if not strip._pills:
            self._hint("No sessions to navigate")
            return
        strip.select_next()
        if self.zoomed:
            self._refresh_zoom()
        else:
            self.zoomed = True

    def action_select_prev(self) -> None:
        if self.queue_visible:
            self.query_one("#approval-queue", ApprovalQueue).move_up()
            return
        strip = self.query_one(SessionStrip)
        if not strip._pills:
            self._hint("No sessions to navigate")
            return
        strip.select_prev()
        if self.zoomed:
            self._refresh_zoom()
        else:
            self.zoomed = True

    def action_queue_up(self) -> None:
        if self.queue_visible:
            self.query_one("#approval-queue", ApprovalQueue).move_up()

    def action_queue_down(self) -> None:
        if self.queue_visible:
            self.query_one("#approval-queue", ApprovalQueue).move_down()

    def action_focus_session(self) -> None:
        """Enter: focus (switch to) the selected session's Ghostty tab."""
        if self.queue_visible:
            self._approve_queue_selected()
            return
        strip = self.query_one(SessionStrip)
        session = strip.selected_session
        if session is None:
            self._hint("No session selected")
            return

        async def _focus():
            try:
                ok = await self._client.focus(session.session_id)
                if ok:
                    self._hint(f"Focused: {session.project_name or session.session_id}")
                else:
                    self._hint("No Ghostty tab for this session")
            except Exception as exc:
                log.debug("focus failed: %s", exc)
                self._hint("Failed to focus session")

        asyncio.create_task(_focus())

    def action_toggle_help(self) -> None:
        """Toggle help overlay showing keybindings."""
        help_lines = [
            "CSM Help",
            "\u2500" * 40,
            "",
            "  \u2190 / \u2192     Navigate between sessions",
            "  Enter        Focus (switch to) session's Ghostty tab",
            "  a            Toggle autopilot",
            "  y            Approve pending tool call",
            "  n            Reject pending tool call",
            "  Q            Toggle approval queue",
            "  A            Approve all safe tool calls",
            "  h            Toggle this help",
            "  Esc          Close help / close queue",
            "  q            Quit",
            "",
            "\u2500" * 40,
            "Autopilot auto-approves safe tools.",
            "Destructive commands always need manual approval.",
            "",
            "\u25b6 running  \u23f8 waiting  \u2714 idle  \u25cf stopped",
        ]
        self._hint("\n".join(help_lines))

    def action_collapse(self) -> None:
        """Escape: close queue if visible, otherwise collapse zoom."""
        if self.queue_visible:
            self.queue_visible = False
        else:
            self.zoomed = False

    def action_toggle_autopilot(self) -> None:
        """Toggle autopilot on the selected session via daemon."""
        strip = self.query_one(SessionStrip)
        session = strip.selected_session
        if session is None:
            self._hint("No session selected")
            return

        async def _toggle():
            try:
                new_state = await self._client.toggle_autopilot(session.session_id)
                state_str = "ON" if new_state else "OFF"
                name = session.project_name or session.session_id
                self._hint(f"Autopilot {state_str} for {name}")
            except Exception as exc:
                log.debug("toggle_autopilot failed: %s", exc)
                self._hint("Failed to toggle autopilot")

        asyncio.create_task(_toggle())

    def action_toggle_queue(self) -> None:
        """Toggle the approval queue overlay."""
        self.queue_visible = not self.queue_visible

    def action_approve(self) -> None:
        """Approve pending tool call on the selected session."""
        strip = self.query_one(SessionStrip)
        session = strip.selected_session
        if session is None:
            self._hint("No session selected")
            return

        name = session.project_name or session.session_id or "?"

        async def _approve():
            try:
                await self._client.approve(session.session_id)
                self._hint(f"Approved tool call in {name}")
            except Exception as exc:
                log.debug("approve failed: %s", exc)
                self._hint("Failed to approve")

        asyncio.create_task(_approve())

    def action_reject(self) -> None:
        """Reject pending tool call on the selected session."""
        strip = self.query_one(SessionStrip)
        session = strip.selected_session
        if session is None:
            self._hint("No session selected")
            return

        name = session.project_name or session.session_id or "?"

        async def _reject():
            try:
                await self._client.reject(session.session_id)
                self._hint(f"Rejected tool call in {name}")
            except Exception as exc:
                log.debug("reject failed: %s", exc)
                self._hint("Failed to reject")

        asyncio.create_task(_reject())

    def action_approve_all_safe(self) -> None:
        """Approve all safe pending tools across all sessions."""
        if self.queue_visible:
            queue = self.query_one("#approval-queue", ApprovalQueue)
            rows = queue.get_all_safe_rows()
            if not rows:
                self._hint("No safe pending tools to approve")
                return

            # Collect unique session IDs to approve.
            seen: set[str] = set()
            sids: list[str] = []
            for row in rows:
                sid = row.session.session_id
                if sid not in seen:
                    seen.add(sid)
                    sids.append(sid)

            async def _bulk_approve():
                count = 0
                for sid in sids:
                    try:
                        await self._client.approve(sid)
                        count += 1
                    except Exception:
                        pass
                self._hint(f"Approved safe tools in {count} session(s)")

            asyncio.create_task(_bulk_approve())
            self._hint(f"Approving {len(rows)} safe tool call(s)...")
            return

        # Fallback: approve all sessions that have only safe pending tools.
        sids = []
        for session in self._sessions:
            if not session.pending_tools:
                continue
            from csm.models import ToolSafety
            if any(pt.safety == ToolSafety.DESTRUCTIVE for pt in session.pending_tools):
                continue
            sids.append(session.session_id)

        if not sids:
            self._hint("No safe pending tools to approve")
            return

        async def _bulk():
            count = 0
            for sid in sids:
                try:
                    await self._client.approve(sid)
                    count += 1
                except Exception:
                    pass
            self._hint(f"Approved safe tools in {count} session(s)")

        asyncio.create_task(_bulk())
        self._hint(f"Approving safe tools in {len(sids)} session(s)...")

    def _approve_queue_selected(self) -> None:
        """Approve the currently selected row in the approval queue."""
        queue = self.query_one("#approval-queue", ApprovalQueue)
        row = queue.approve_selected()
        if row is None:
            self._hint("Nothing to approve")
            return

        sid = row.session.session_id
        name = row.session.project_name or row.session.session_id or "?"

        async def _approve():
            try:
                await self._client.approve(sid)
                self._hint(f"Approved {row.tool_name} in {name}")
            except Exception as exc:
                log.debug("approve failed: %s", exc)
                self._hint("Failed to approve")

        asyncio.create_task(_approve())

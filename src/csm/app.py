"""Main Textual TUI application for Claude Session Manager."""

from __future__ import annotations

import logging
import os
import random
import time
from pathlib import Path

from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.reactive import reactive
from textual.widgets import Static

from csm import ghostty
from csm.autopilot import ToolSafety, classify_pending_tools
from csm.notifications import SessionNotifier
from csm.parser import extract_pending_tool, read_tail
from csm.scanner import discover_sessions
from csm.watcher import Watcher
from csm.widgets.approval_queue import ApprovalQueue
from csm.widgets.session_strip import SessionStrip
from csm.widgets.zoom_panel import ZoomPanel

log = logging.getLogger(__name__)

HINTS = [
    "Arrow keys: navigate sessions",
    "'q' to quit",
    "Enter: switch to Ghostty tab",
    "Escape: collapse zoom panel",
    "Press 'a' to toggle autopilot for the selected session",
    "Press 'Q' for the approval queue overlay",
    "Press 'y' to approve the pending tool call",
    "Press 'n' to reject the pending tool call",
    "Press 'A' to approve all safe tool calls at once",
    "Autopilot auto-approves safe tools, pauses on destructive ones",
    "Destructive commands (git push, rm, etc.) always need manual approval",
    "Sessions marked [self] are this CSM instance",
    "Strict mode: destructive = git push, rm, npm publish, ...",
]

HINT_ROTATE_SECS = 45

# Label appended to the project name of the session running CSM itself.
SELF_LABEL = " [self]"


def _is_self_session(session, own_cwd: str) -> bool:
    """Return True if *session* belongs to the process running CSM."""
    if not own_cwd or not session.project_path:
        return False
    return session.project_path.rstrip("/") == own_cwd.rstrip("/")


class SessionManagerApp(App):
    """Compact TUI for monitoring Claude Code sessions."""

    CSS_PATH = Path("css/app.tcss")
    TITLE = "csm"

    BINDINGS = [
        Binding("left", "select_prev", "Prev session", show=False, priority=True),
        Binding("right", "select_next", "Next session", show=False, priority=True),
        Binding("q", "quit", "Quit"),
        Binding("enter", "switch_tab_or_approve", "Switch tab / Approve", show=False),
        Binding("escape", "collapse", "Collapse", show=False),
        Binding("a", "toggle_autopilot", "Autopilot", show=False),
        Binding("Q", "toggle_queue", "Queue", show=False),
        Binding("y", "approve", "Approve", show=False),
        Binding("n", "reject", "Reject", show=False),
        Binding("A", "approve_all_safe", "Approve all safe", show=False),
        Binding("up", "queue_up", "Queue up", show=False, priority=True),
        Binding("down", "queue_down", "Queue down", show=False, priority=True),
    ]

    zoomed: reactive[bool] = reactive(False)
    queue_visible: reactive[bool] = reactive(False)

    # Cooldown (seconds) between autopilot approvals for the same session.
    # Gives CC time to process and write to JSONL before we re-poll.
    _AUTOPILOT_COOLDOWN = 10.0

    def __init__(self, **kwargs) -> None:
        super().__init__(**kwargs)
        # Per-session autopilot state — survives across Session object recreation.
        self._autopilot_state: dict[str, bool] = {}
        # Last known sessions for autopilot loop.
        self._sessions: list = []
        # Timestamp of the last auto-approval per session (cooldown guard).
        self._last_approved: dict[str, float] = {}
        # Incremental file watcher (replaces raw discover_sessions() calls).
        self._watcher = Watcher()
        # Desktop notification manager.
        self._notifier = SessionNotifier()
        # Our own cwd for self-exclusion marking.
        self._own_cwd = os.getcwd()

    def compose(self) -> ComposeResult:
        yield Static(id="main-area")
        yield ZoomPanel(id="zoom-panel")
        yield ApprovalQueue(id="approval-queue")
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
        sessions = self._watcher.poll()

        # Apply persisted autopilot state to freshly-created Session objects,
        # mark self-session, and pre-compute destructive-pending flag.
        for session in sessions:
            if session.session_id in self._autopilot_state:
                session.autopilot = self._autopilot_state[session.session_id]

            # Self-exclusion: tag sessions running in the same cwd as CSM.
            if _is_self_session(session, self._own_cwd):
                session.is_self = True

            if session.autopilot:
                try:
                    entries = self._watcher.get_entries(session.jsonl_path)
                    if not entries:
                        entries = read_tail(session.jsonl_path, n_lines=50)
                    pending = extract_pending_tool(entries)
                    if pending:
                        classified = classify_pending_tools(pending)
                        session.has_destructive_pending = any(
                            s == ToolSafety.DESTRUCTIVE for _, _, s in classified
                        )
                except Exception:
                    pass

        # Fire desktop notifications for state transitions.
        self._notifier.check(sessions)

        self._sessions = sessions

        # Handle empty session list edge case.
        if not sessions:
            self.call_from_thread(self._show_empty_state)
        else:
            strip = self.query_one(SessionStrip)
            self.call_from_thread(strip.update_sessions, sessions)

        # If zoomed, refresh the zoom panel with the currently selected session
        if self.zoomed:
            self.call_from_thread(self._refresh_zoom)

        # If queue is visible, refresh it
        if self.queue_visible:
            queue = self.query_one("#approval-queue", ApprovalQueue)
            self.call_from_thread(queue.update_queue, sessions)

        # Run the autopilot loop (auto-approve safe pending tools).
        self._run_autopilot(sessions)

    def _show_empty_state(self) -> None:
        """Update UI when no sessions are discovered."""
        strip = self.query_one(SessionStrip)
        strip.update_sessions([])
        self._hint("No Claude sessions found. Start a Claude Code session to begin.")

    def _run_autopilot(self, sessions: list) -> None:
        """Auto-approve safe/unknown pending tools for sessions with autopilot on."""
        now = time.monotonic()
        for session in sessions:
            if not session.autopilot:
                continue
            if not session.ghostty_tab_name:
                continue

            # Cooldown: skip if we recently sent an approval for this session.
            last_ts = self._last_approved.get(session.session_id, 0.0)
            if now - last_ts < self._AUTOPILOT_COOLDOWN:
                log.debug(
                    "Autopilot: session %s still in cooldown — skipping",
                    session.session_id,
                )
                continue

            try:
                entries = self._watcher.get_entries(session.jsonl_path)
                if not entries:
                    entries = read_tail(session.jsonl_path, n_lines=50)
                pending = extract_pending_tool(entries)
            except Exception:
                continue

            if not pending:
                continue

            classified = classify_pending_tools(pending)
            # If ALL pending tools are safe or unknown, auto-approve.
            # If any is destructive, skip — user must manually approve.
            has_destructive = any(
                s == ToolSafety.DESTRUCTIVE for _, _, s in classified
            )
            if has_destructive:
                log.debug(
                    "Autopilot: session %s has destructive pending tool — skipping",
                    session.session_id,
                )
                continue

            # All are safe/unknown — send approval.
            tab = session.ghostty_tab_name
            log.debug("Autopilot: auto-approving for session %s", session.session_id)
            ghostty.send_approval(tab)
            self._last_approved[session.session_id] = now

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

    def action_quit(self) -> None:
        """Clean up watcher and notifier state, then quit."""
        self._watcher.clear()
        self._notifier.clear()
        super().action_quit()

    # ── Actions ──────────────────────────────────────────────

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

    def action_switch_tab_or_approve(self) -> None:
        """Enter: approve selected queue item, or switch Ghostty tab."""
        if self.queue_visible:
            self._approve_queue_selected()
            return
        self._switch_tab()

    def _switch_tab(self) -> None:
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
            self._hint("No Ghostty tab found for this session")

    def action_collapse(self) -> None:
        """Escape: close queue if visible, otherwise collapse zoom."""
        if self.queue_visible:
            self.queue_visible = False
        else:
            self.zoomed = False

    def action_toggle_autopilot(self) -> None:
        """Toggle autopilot on the selected session."""
        strip = self.query_one(SessionStrip)
        session = strip.selected_session
        if session is None:
            self._hint("No session selected")
            return

        session.autopilot = not session.autopilot
        self._autopilot_state[session.session_id] = session.autopilot

        state_str = "ON" if session.autopilot else "OFF"
        name = session.project_name or session.slug or session.session_id
        self._hint(f"Autopilot {state_str} for {name}")

        # Refresh pill display immediately.
        strip = self.query_one(SessionStrip)
        for pill in strip._pills:
            if pill.session.session_id == session.session_id:
                pill.update_session(session)
                break

        if self.zoomed:
            self._refresh_zoom()

    def action_toggle_queue(self) -> None:
        """Toggle the approval queue overlay."""
        self.queue_visible = not self.queue_visible

    def action_approve(self) -> None:
        """Approve pending tool call on the selected (zoomed) session."""
        strip = self.query_one(SessionStrip)
        session = strip.selected_session
        if session is None:
            self._hint("No session selected")
            return
        if not session.ghostty_tab_name:
            self._hint("No Ghostty tab for this session")
            return
        tab = session.ghostty_tab_name
        self.run_worker(lambda: ghostty.send_approval(tab), thread=True)
        name = session.project_name or session.slug or "?"
        self._hint(f"Approved tool call in {name}")

    def action_reject(self) -> None:
        """Reject pending tool call on the selected (zoomed) session."""
        strip = self.query_one(SessionStrip)
        session = strip.selected_session
        if session is None:
            self._hint("No session selected")
            return
        if not session.ghostty_tab_name:
            self._hint("No Ghostty tab for this session")
            return
        tab = session.ghostty_tab_name
        self.run_worker(lambda: ghostty.send_rejection(tab), thread=True)
        name = session.project_name or session.slug or "?"
        self._hint(f"Rejected tool call in {name}")

    def action_approve_all_safe(self) -> None:
        """Approve all safe pending tools across all sessions."""
        if self.queue_visible:
            queue = self.query_one("#approval-queue", ApprovalQueue)
            rows = queue.get_all_safe_rows()
            if not rows:
                self._hint("No safe pending tools to approve")
                return
            # Approve each in a background worker (sequentially to avoid
            # tab-switching conflicts).
            def _bulk_approve():
                count = 0
                seen_tabs: set[str] = set()
                for row in rows:
                    tab = row.session.ghostty_tab_name
                    if not tab or tab in seen_tabs:
                        continue
                    seen_tabs.add(tab)
                    ghostty.send_approval(tab)
                    count += 1
                return count

            self.run_worker(_bulk_approve, thread=True)
            self._hint(f"Approving {len(rows)} safe tool call(s)...")
            return

        # Fallback: approve all safe across all known sessions.
        count = 0
        for session in self._sessions:
            if not session.ghostty_tab_name:
                continue
            try:
                entries = self._watcher.get_entries(session.jsonl_path)
                if not entries:
                    entries = read_tail(session.jsonl_path, n_lines=50)
                pending = extract_pending_tool(entries)
            except Exception:
                continue
            if not pending:
                continue
            classified = classify_pending_tools(pending)
            if any(s == ToolSafety.DESTRUCTIVE for _, _, s in classified):
                continue
            tab = session.ghostty_tab_name

            def _approve(t=tab):
                ghostty.send_approval(t)

            self.run_worker(_approve, thread=True)
            count += 1

        if count:
            self._hint(f"Approving safe tools in {count} session(s)...")
        else:
            self._hint("No safe pending tools to approve")

    def _approve_queue_selected(self) -> None:
        """Approve the currently selected row in the approval queue."""
        queue = self.query_one("#approval-queue", ApprovalQueue)
        row = queue.approve_selected()
        if row is None:
            self._hint("Nothing to approve")
            return
        tab = row.session.ghostty_tab_name
        if not tab:
            self._hint("No Ghostty tab for this session")
            return
        self.run_worker(lambda: ghostty.send_approval(tab), thread=True)
        name = row.session.project_name or row.session.slug or "?"
        self._hint(f"Approved {row.tool_name} in {name}")

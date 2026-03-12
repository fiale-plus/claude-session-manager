"""Approval queue overlay — shows all pending tool approvals across sessions."""

from __future__ import annotations

from textual.containers import Vertical
from textual.message import Message
from textual.reactive import reactive
from textual.widget import Widget
from textual.widgets import Static

from csm.autopilot import ToolSafety, classify_pending_tools
from csm.models import Session
from csm.parser import extract_pending_tool, read_tail


class ApprovalQueue(Vertical):
    """Panel listing all pending tool approvals with safety classification."""

    DEFAULT_CSS = ""

    class ApproveRequest(Message):
        """Fired when the user approves a specific pending tool."""

        def __init__(self, session: Session, tool_name: str, tool_input: dict) -> None:
            super().__init__()
            self.session = session
            self.tool_name = tool_name
            self.tool_input = tool_input

    class ApproveAllSafe(Message):
        """Fired when the user wants to approve all safe pending tools."""

    selected_index: reactive[int] = reactive(0)

    def __init__(self, **kwargs) -> None:
        super().__init__(**kwargs)
        self._rows: list[_QueueRow] = []

    def compose(self):
        yield Static(id="queue-header")
        yield Static(id="queue-body")
        yield Static(id="queue-footer")

    def update_queue(self, sessions: list[Session]) -> None:
        """Rebuild the queue from the current session list."""
        rows: list[_QueueRow] = []
        for session in sessions:
            pending = extract_pending_tool(
                read_tail(session.jsonl_path, n_lines=50)
            )
            if not pending:
                continue
            classified = classify_pending_tools(pending)
            for tool_name, tool_input, safety in classified:
                rows.append(_QueueRow(
                    session=session,
                    tool_name=tool_name,
                    tool_input=tool_input,
                    safety=safety,
                    auto_approved=(
                        session.autopilot
                        and safety in (ToolSafety.SAFE, ToolSafety.UNKNOWN)
                    ),
                ))
        self._rows = rows

        # Clamp index
        if self._rows:
            self.selected_index = min(self.selected_index, len(self._rows) - 1)
        else:
            self.selected_index = 0

        self._refresh_display()

    def _refresh_display(self) -> None:
        """Render header, body, and footer from current rows."""
        header = self.query_one("#queue-header", Static)
        body = self.query_one("#queue-body", Static)
        footer = self.query_one("#queue-footer", Static)

        if not self._rows:
            header.update(" PENDING APPROVALS                        (none)")
            body.update("  (no pending tool calls)")
            footer.update("")
            return

        n_safe = sum(
            1 for r in self._rows
            if r.safety in (ToolSafety.SAFE, ToolSafety.UNKNOWN)
        )
        n_manual = len(self._rows) - n_safe
        header.update(
            f" PENDING APPROVALS"
            f"                        {n_safe} safe, {n_manual} manual"
        )

        separator = " " + "\u2500" * 56
        lines = [separator]
        for i, row in enumerate(self._rows):
            marker = "\u25B6" if i == self.selected_index else " "
            icon = _safety_icon(row.safety, row.auto_approved)
            name = (row.session.project_name or row.session.slug or "?")[:8].ljust(8)
            tool_desc = _tool_summary(row.tool_name, row.tool_input)
            tag = _safety_tag(row.safety, row.auto_approved)
            lines.append(f" {marker} {icon} {name} {tool_desc:<36s} {tag}")
        lines.append(separator)
        body.update("\n".join(lines))

        footer.update(
            " [Enter] approve selected  "
            "[A] approve all safe  "
            "[Esc] close"
        )

    def watch_selected_index(self, value: int) -> None:
        self._refresh_display()

    def move_up(self) -> None:
        if self._rows:
            self.selected_index = (self.selected_index - 1) % len(self._rows)

    def move_down(self) -> None:
        if self._rows:
            self.selected_index = (self.selected_index + 1) % len(self._rows)

    def approve_selected(self) -> _QueueRow | None:
        """Return the currently-selected row (caller handles the approval)."""
        if not self._rows or self.selected_index >= len(self._rows):
            return None
        return self._rows[self.selected_index]

    def get_all_safe_rows(self) -> list[_QueueRow]:
        """Return all rows that are safe or unknown (auto-approvable)."""
        return [
            r for r in self._rows
            if r.safety in (ToolSafety.SAFE, ToolSafety.UNKNOWN)
        ]

    @property
    def row_count(self) -> int:
        return len(self._rows)


class _QueueRow:
    """Internal data holder for one queue entry."""

    __slots__ = ("session", "tool_name", "tool_input", "safety", "auto_approved")

    def __init__(
        self,
        session: Session,
        tool_name: str,
        tool_input: dict,
        safety: ToolSafety,
        auto_approved: bool,
    ) -> None:
        self.session = session
        self.tool_name = tool_name
        self.tool_input = tool_input
        self.safety = safety
        self.auto_approved = auto_approved


def _safety_icon(safety: ToolSafety, auto_approved: bool) -> str:
    if auto_approved:
        return "\u2713"  # ✓
    if safety == ToolSafety.DESTRUCTIVE:
        return "\u26A0"  # ⚠
    return "\u2022"  # •


def _safety_tag(safety: ToolSafety, auto_approved: bool) -> str:
    if auto_approved:
        return "[auto-approved]"
    if safety == ToolSafety.DESTRUCTIVE:
        return "NEEDS APPROVAL"
    return ""


def _tool_summary(tool_name: str, tool_input: dict) -> str:
    """Short one-line description of a tool call."""
    if tool_name == "Bash":
        cmd = str(tool_input.get("command", ""))[:40]
        return f"Bash {cmd}"
    if "file_path" in tool_input:
        return f"{tool_name} {tool_input['file_path']}"
    if "pattern" in tool_input:
        return f"{tool_name} {str(tool_input['pattern'])[:30]}"
    if "command" in tool_input:
        return f"{tool_name} {str(tool_input['command'])[:30]}"
    return tool_name

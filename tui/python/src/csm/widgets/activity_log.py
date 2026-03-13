"""Activity timeline widget — recent tool calls and messages."""

from __future__ import annotations

from textual.widget import Widget

from csm.models import Activity, ActivityType

ACTIVITY_ICONS: dict[ActivityType, str] = {
    ActivityType.TOOL_USE: "\u2699",       # ⚙
    ActivityType.TEXT: "\U0001F4AC",        # 💬
    ActivityType.USER_MESSAGE: "\U0001F464",  # 👤
    ActivityType.THINKING: "\u23F3",        # ⏳
    ActivityType.SYSTEM: "\u2022",          # •
}


class ActivityLog(Widget):
    """Vertical list of recent activity entries."""

    DEFAULT_CSS = ""

    def __init__(self, **kwargs) -> None:
        super().__init__(**kwargs)
        self._activities: list[Activity] = []

    def update_activities(self, activities: list[Activity]) -> None:
        """Replace the activity list and refresh."""
        self._activities = list(activities)
        self.refresh()

    def render(self) -> str:
        if not self._activities:
            return "  (no recent activity)"
        lines: list[str] = []
        for act in self._activities:
            ts = act.timestamp.strftime("%H:%M")
            icon = ACTIVITY_ICONS.get(act.activity_type, " ")
            summary = act.summary[:70]
            lines.append(f"  {ts} {icon} {summary}")
        return "\n".join(lines)

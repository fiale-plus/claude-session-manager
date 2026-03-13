"""Domain objects for the CSM TUI client.

Simplified from the original — sessions now come as JSON from the daemon.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum


class SessionState(Enum):
    RUNNING = "running"
    WAITING = "waiting"
    IDLE = "idle"
    DEAD = "dead"


class ActivityType(Enum):
    TOOL_USE = "tool_use"
    TEXT = "text"
    THINKING = "thinking"
    USER_MESSAGE = "user_message"
    SYSTEM = "system"


class ToolSafety(Enum):
    SAFE = "safe"
    DESTRUCTIVE = "destructive"
    UNKNOWN = "unknown"


@dataclass
class PendingTool:
    """A tool call pending approval, as reported by the daemon."""

    tool_name: str
    tool_input: dict = field(default_factory=dict)
    safety: ToolSafety = ToolSafety.UNKNOWN


@dataclass
class Activity:
    timestamp: datetime
    activity_type: ActivityType
    summary: str
    detail: str = ""


@dataclass
class Session:
    session_id: str
    cwd: str
    project_name: str
    state: SessionState
    autopilot: bool = False
    has_destructive: bool = False
    pending_tools: list[PendingTool] = field(default_factory=list)
    ghostty_tab: str = ""
    git_branch: str = ""
    last_text: str = ""
    activities: list[Activity] = field(default_factory=list)
    last_activity_time: datetime | None = None
    pid: int | None = None


def session_from_dict(d: dict) -> Session:
    """Deserialize a session from the daemon's JSON representation."""
    state = SessionState(d.get("state", "idle"))

    activities: list[Activity] = []
    for a in d.get("activities", []):
        ts_str = a.get("timestamp", "")
        try:
            ts = datetime.fromisoformat(ts_str.replace("Z", "+00:00"))
        except (ValueError, TypeError):
            continue
        try:
            atype = ActivityType(a.get("activity_type", "system"))
        except ValueError:
            atype = ActivityType.SYSTEM
        activities.append(Activity(
            timestamp=ts,
            activity_type=atype,
            summary=a.get("summary", ""),
            detail=a.get("detail", ""),
        ))

    pending: list[PendingTool] = []
    for pt in d.get("pending_tools", []):
        try:
            safety = ToolSafety(pt.get("safety", "unknown"))
        except ValueError:
            safety = ToolSafety.UNKNOWN
        pending.append(PendingTool(
            tool_name=pt.get("tool_name", "?"),
            tool_input=pt.get("tool_input", {}),
            safety=safety,
        ))

    last_activity_str = d.get("last_activity_time", "")
    last_activity_time = None
    if last_activity_str:
        try:
            last_activity_time = datetime.fromisoformat(
                last_activity_str.replace("Z", "+00:00")
            )
        except (ValueError, TypeError):
            pass

    return Session(
        session_id=d.get("session_id", ""),
        cwd=d.get("cwd", ""),
        project_name=d.get("project_name", ""),
        state=state,
        autopilot=d.get("autopilot", False),
        has_destructive=d.get("has_destructive", False),
        pending_tools=pending,
        ghostty_tab=d.get("ghostty_tab", ""),
        git_branch=d.get("git_branch", ""),
        last_text=d.get("last_text", ""),
        activities=activities,
        last_activity_time=last_activity_time,
        pid=d.get("pid"),
    )

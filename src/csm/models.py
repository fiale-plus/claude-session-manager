"""Domain objects for Claude Session Manager."""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum
from pathlib import Path


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


@dataclass
class Activity:
    timestamp: datetime
    activity_type: ActivityType
    summary: str
    detail: str = ""


@dataclass
class Session:
    session_id: str
    slug: str
    project_path: str
    project_name: str
    jsonl_path: Path
    state: SessionState
    last_activity_time: datetime | None = None
    activities: list[Activity] = field(default_factory=list)
    last_text: str = ""  # motivation — last assistant text
    ghostty_tab_index: int | None = None
    ghostty_tab_name: str = ""
    pid: int | None = None
    git_branch: str = ""
    autopilot: bool = False
    has_destructive_pending: bool = False

"""CLI entry point for Claude Session Manager."""

from __future__ import annotations

import argparse
import json
import sys
from datetime import datetime

from csm.scanner import discover_sessions


def _session_to_dict(session) -> dict:
    """Convert a Session to a JSON-serializable dict."""
    return {
        "session_id": session.session_id,
        "slug": session.slug,
        "project_path": session.project_path,
        "project_name": session.project_name,
        "jsonl_path": str(session.jsonl_path),
        "state": session.state.value,
        "last_activity_time": (
            session.last_activity_time.isoformat()
            if session.last_activity_time
            else None
        ),
        "last_text": session.last_text,
        "pid": session.pid,
        "git_branch": session.git_branch,
        "autopilot": session.autopilot,
        "activities": [
            {
                "timestamp": a.timestamp.isoformat(),
                "type": a.activity_type.value,
                "summary": a.summary,
            }
            for a in session.activities
        ],
    }


def main():
    parser = argparse.ArgumentParser(
        prog="csm",
        description="Claude Session Manager — monitor and manage Claude Code sessions",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Discover sessions and print as JSON, then exit",
    )
    args = parser.parse_args()

    if args.dry_run:
        sessions = discover_sessions()
        output = [_session_to_dict(s) for s in sessions]
        print(json.dumps(output, indent=2))
        sys.exit(0)

    print("TUI coming soon")


if __name__ == "__main__":
    main()

"""Async client for the CSM daemon control socket.

Connects to /tmp/csm-ctl.sock and speaks NDJSON.
"""

from __future__ import annotations

import asyncio
import json
import logging
from collections.abc import AsyncIterator
from pathlib import Path

from csm.models import Session, session_from_dict

log = logging.getLogger(__name__)

DEFAULT_SOCKET_PATH = Path("/tmp/csm-ctl.sock")


class DaemonClient:
    """Async client connecting to /tmp/csm-ctl.sock."""

    def __init__(self, socket_path: Path = DEFAULT_SOCKET_PATH) -> None:
        self._socket_path = socket_path
        self._reader: asyncio.StreamReader | None = None
        self._writer: asyncio.StreamWriter | None = None

    async def connect(self) -> None:
        """Open the Unix socket connection."""
        self._reader, self._writer = await asyncio.open_unix_connection(
            str(self._socket_path)
        )
        log.debug("Connected to daemon at %s", self._socket_path)

    async def _send(self, msg: dict) -> None:
        """Send an NDJSON message."""
        if self._writer is None:
            raise RuntimeError("Not connected")
        line = json.dumps(msg, separators=(",", ":")) + "\n"
        self._writer.write(line.encode())
        await self._writer.drain()

    async def _recv(self) -> dict:
        """Read one NDJSON line and parse it."""
        if self._reader is None:
            raise RuntimeError("Not connected")
        line = await self._reader.readline()
        if not line:
            raise ConnectionError("Daemon closed connection")
        return json.loads(line)

    async def subscribe(self) -> AsyncIterator[list[Session]]:
        """Subscribe to session updates.

        Yields a list of Session objects on every update.
        The first yield is the initial snapshot; subsequent yields are
        pushed by the daemon whenever sessions change.
        """
        await self._send({"action": "subscribe"})
        while True:
            msg = await self._recv()
            event = msg.get("event")
            if event == "sessions_updated":
                raw_sessions = msg.get("sessions", [])
                sessions = [session_from_dict(s) for s in raw_sessions]
                yield sessions
            elif msg.get("error"):
                log.error("Daemon error: %s", msg["error"])
            # Ignore unknown events gracefully.

    async def toggle_autopilot(self, session_id: str) -> bool:
        """Toggle autopilot for a session. Returns the new autopilot state."""
        await self._send({
            "action": "toggle_autopilot",
            "session_id": session_id,
        })
        resp = await self._recv()
        return resp.get("autopilot", False)

    async def approve(self, session_id: str) -> bool:
        """Approve the pending tool call for a session."""
        await self._send({
            "action": "approve",
            "session_id": session_id,
        })
        resp = await self._recv()
        return resp.get("ok", False)

    async def reject(self, session_id: str) -> bool:
        """Reject the pending tool call for a session."""
        await self._send({
            "action": "reject",
            "session_id": session_id,
        })
        resp = await self._recv()
        return resp.get("ok", False)

    async def disconnect(self) -> None:
        """Close the socket connection."""
        if self._writer is not None:
            self._writer.close()
            try:
                await self._writer.wait_closed()
            except Exception:
                pass
            self._writer = None
            self._reader = None
            log.debug("Disconnected from daemon")

import React, { useState, useEffect, useRef, useCallback } from "react";
import { Box, useInput, useApp } from "ink";
import { CSMClient } from "../client/client.js";
import type { Session } from "../client/client.js";
import { ZoomPanel } from "./ZoomPanel.js";
import { Strip } from "./Strip.js";
import { HintsBar } from "./HintsBar.js";
import { Queue } from "./Queue.js";

export function App() {
  const { exit } = useApp();
  const [sessions, setSessions] = useState<Session[]>([]);
  const [selectedIdx, setSelectedIdx] = useState(0);
  const [queueVisible, setQueueVisible] = useState(false);
  const clientRef = useRef<CSMClient | null>(null);

  useEffect(() => {
    const client = new CSMClient();
    clientRef.current = client;

    client.on("sessions", (updated: Session[]) => {
      setSessions(updated);
    });

    client.on("error", () => {
      // silently reconnect or ignore
    });

    client.connect();

    return () => {
      client.disconnect();
    };
  }, []);

  const selectedSession = sessions.length > 0 ? sessions[selectedIdx] ?? null : null;

  const handleApprove = useCallback(
    (sessionId: string) => {
      clientRef.current?.approve(sessionId);
    },
    []
  );

  const handleReject = useCallback(
    (sessionId: string) => {
      clientRef.current?.reject(sessionId);
    },
    []
  );

  useInput((input, key) => {
    // Navigation
    if (key.leftArrow || input === "h") {
      setSelectedIdx((i) => Math.max(0, i - 1));
      return;
    }
    if (key.rightArrow || input === "l") {
      setSelectedIdx((i) => Math.min(sessions.length - 1, i + 1));
      return;
    }

    // Quit
    if (input === "q" && !key.shift) {
      exit();
      process.exit(0);
    }

    // Toggle autopilot
    if (input === "a" && !key.shift) {
      if (selectedSession) {
        clientRef.current?.toggleAutopilot(selectedSession.session_id);
      }
      return;
    }

    // Toggle queue
    if (input === "Q" || (input === "q" && key.shift)) {
      setQueueVisible((v) => !v);
      return;
    }

    // Close queue on escape
    if (key.escape) {
      setQueueVisible(false);
      return;
    }

    // Approve
    if (key.return || input === "y") {
      if (selectedSession) {
        handleApprove(selectedSession.session_id);
      }
      return;
    }

    // Reject
    if (input === "n" && !key.shift) {
      if (selectedSession) {
        handleReject(selectedSession.session_id);
      }
      return;
    }

    // Approve all safe
    if (input === "A" || (input === "a" && key.shift)) {
      for (const s of sessions) {
        if (
          s.state === "waiting" &&
          s.pending_tools.length > 0 &&
          s.pending_tools.every((t) => t.safety === "safe")
        ) {
          handleApprove(s.session_id);
        }
      }
      return;
    }
  });

  return (
    <Box flexDirection="column" width="100%">
      <ZoomPanel session={selectedSession} />
      {queueVisible && (
        <Queue
          sessions={sessions}
          onApprove={handleApprove}
          onReject={handleReject}
        />
      )}
      <HintsBar queueVisible={queueVisible} />
      <Strip sessions={sessions} selectedIdx={selectedIdx} />
    </Box>
  );
}

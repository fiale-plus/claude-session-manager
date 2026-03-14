import React, { useState, useEffect, useRef, useCallback } from "react";
import { Box, Text, useInput, useApp } from "ink";
import { CSMClient } from "../client/client.js";
import type { Session } from "../client/client.js";
import { ZoomPanel } from "./ZoomPanel.js";
import { Strip } from "./Strip.js";
import { HintsBar } from "./HintsBar.js";
import { Queue } from "./Queue.js";

function HelpPanel({ width }: { width?: number }) {
  const bindings = [
    ["\u2190 / \u2192", "Navigate between sessions"],
    ["Enter", "Focus (switch to) the selected session's Ghostty tab"],
    ["a", "Toggle autopilot for the selected session"],
    ["y", "Approve the pending tool call"],
    ["n", "Reject the pending tool call"],
    ["Q", "Toggle approval queue overlay"],
    ["A", "Approve all safe pending tool calls"],
    ["h", "Toggle this help screen"],
    ["Esc", "Close help / close queue"],
    ["q", "Quit CSM"],
  ];

  return (
    <Box
      flexDirection="column"
      borderStyle="round"
      borderColor="blue"
      paddingLeft={2}
      paddingRight={2}
      paddingTop={1}
      paddingBottom={1}
      width={width}
    >
      <Text bold color="blue">CSM Help</Text>
      <Text dimColor>{"─".repeat(40)}</Text>
      <Text> </Text>
      {bindings.map(([key, desc], i) => (
        <Text key={i}>
          {"  "}
          <Text bold color="white">{(key ?? "").padEnd(12)}</Text>
          {"  "}
          <Text dimColor>{desc}</Text>
        </Text>
      ))}
      <Text> </Text>
      <Text dimColor>{"─".repeat(40)}</Text>
      <Text> </Text>
      <Text dimColor italic>
        Autopilot auto-approves safe tools. Destructive commands always need manual approval.
      </Text>
      <Text> </Text>
      <Text dimColor>
        {"\u25b6"} running  {"\u23f8"} waiting  {"\u2714"} idle  {"\u25cf"} stopped
      </Text>
    </Box>
  );
}

export function App() {
  const { exit } = useApp();
  const [sessions, setSessions] = useState<Session[]>([]);
  const [selectedIdx, setSelectedIdx] = useState(0);
  const [queueVisible, setQueueVisible] = useState(false);
  const [helpVisible, setHelpVisible] = useState(false);
  const clientRef = useRef<CSMClient | null>(null);
  const selectedSIDRef = useRef<string>("");

  useEffect(() => {
    const client = new CSMClient();
    clientRef.current = client;

    client.on("sessions", (updated: Session[]) => {
      setSessions((prev) => {
        // Restore selection by session ID.
        const sid = selectedSIDRef.current;
        if (sid) {
          const newIdx = updated.findIndex((s) => s.session_id === sid);
          if (newIdx >= 0) {
            setSelectedIdx(newIdx);
          }
        }
        return updated;
      });
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
    if (key.leftArrow) {
      setSelectedIdx((i) => {
        const newIdx = Math.max(0, i - 1);
        if (sessions[newIdx]) {
          selectedSIDRef.current = sessions[newIdx].session_id;
        }
        return newIdx;
      });
      return;
    }
    if (key.rightArrow || input === "l") {
      setSelectedIdx((i) => {
        const newIdx = Math.min(sessions.length - 1, i + 1);
        if (sessions[newIdx]) {
          selectedSIDRef.current = sessions[newIdx].session_id;
        }
        return newIdx;
      });
      return;
    }

    // Quit
    if (input === "q" && !key.shift) {
      if (queueVisible || helpVisible) return;
      exit();
      process.exit(0);
    }

    // Toggle help
    if (input === "h" && !key.shift) {
      setHelpVisible((v) => !v);
      return;
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

    // Close help first, then queue on escape
    if (key.escape) {
      if (helpVisible) {
        setHelpVisible(false);
      } else {
        setQueueVisible(false);
      }
      return;
    }

    // Focus session (Enter)
    if (key.return) {
      if (selectedSession) {
        clientRef.current?.focus(selectedSession.session_id);
      }
      return;
    }

    // Approve (y only, not enter)
    if (input === "y" && !key.shift) {
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
      {helpVisible ? (
        <HelpPanel />
      ) : (
        <>
          <ZoomPanel session={selectedSession} />
          {queueVisible && (
            <Queue
              sessions={sessions}
              onApprove={handleApprove}
              onReject={handleReject}
            />
          )}
        </>
      )}
      <HintsBar queueVisible={queueVisible} />
      <Strip sessions={sessions} selectedIdx={selectedIdx} />
    </Box>
  );
}

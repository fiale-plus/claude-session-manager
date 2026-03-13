import React from "react";
import { Box, Text } from "ink";
import type { Session, PendingTool } from "../client/client.js";

interface QueueProps {
  sessions: Session[];
  onApprove: (sessionId: string) => void;
  onReject: (sessionId: string) => void;
}

function safetyIcon(safety: PendingTool["safety"]): string {
  switch (safety) {
    case "safe":
      return "\u2713";
    case "destructive":
      return "\u26A0";
    default:
      return "\u2022";
  }
}

function safetyColor(safety: PendingTool["safety"]): string {
  switch (safety) {
    case "safe":
      return "green";
    case "destructive":
      return "redBright";
    default:
      return "yellow";
  }
}

export function Queue({ sessions }: QueueProps) {
  const waiting = sessions.filter(
    (s) => s.state === "waiting" && s.pending_tools.length > 0
  );

  if (waiting.length === 0) {
    return (
      <Box
        flexDirection="column"
        padding={1}
        borderStyle="double"
        borderColor="yellow"
      >
        <Text bold>Approval Queue</Text>
        <Text dimColor>No pending approvals</Text>
      </Box>
    );
  }

  return (
    <Box
      flexDirection="column"
      padding={1}
      borderStyle="double"
      borderColor="yellow"
    >
      <Text bold>Approval Queue</Text>
      {waiting.map((s) => (
        <Box key={s.session_id} flexDirection="column" marginTop={1}>
          <Text bold color="cyan">
            {s.project_name}
          </Text>
          {s.pending_tools.map((tool, i) => (
            <Box key={i} paddingLeft={2}>
              <Text color={safetyColor(tool.safety)}>
                {safetyIcon(tool.safety)}{" "}
              </Text>
              <Text>
                {tool.tool_name}
                {tool.tool_input.command
                  ? `: ${String(tool.tool_input.command).slice(0, 60)}`
                  : ""}
              </Text>
            </Box>
          ))}
        </Box>
      ))}
    </Box>
  );
}

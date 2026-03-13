import React from "react";
import { Box, Text } from "ink";
import type { Session } from "../client/client.js";

interface ZoomPanelProps {
  session: Session | null;
}

export function ZoomPanel({ session }: ZoomPanelProps) {
  if (!session) {
    return (
      <Box flexDirection="column" padding={1}>
        <Text dimColor>No sessions connected</Text>
      </Box>
    );
  }

  const stateIcon: Record<Session["state"], string> = {
    running: "\u2699",
    waiting: "\u23F3",
    idle: "\u2713",
    dead: "\u25CB",
  };

  const stateColor: Record<Session["state"], string> = {
    running: "green",
    waiting: "yellow",
    idle: "gray",
    dead: "blackBright",
  };

  const recentActivities = session.activities.slice(-6);

  return (
    <Box flexDirection="column" padding={1} borderStyle="single" borderColor="gray">
      {/* Header */}
      <Box>
        <Text bold color={stateColor[session.state]}>
          {stateIcon[session.state]} {session.project_name}
        </Text>
        <Text dimColor> ({session.state})</Text>
        {session.git_branch && (
          <Text color="cyan"> \u25B8 {session.git_branch}</Text>
        )}
        {session.autopilot && (
          <Text color={session.has_destructive ? "redBright" : "greenBright"}>
            {" "}[autopilot]
          </Text>
        )}
      </Box>

      {/* CWD */}
      <Box marginTop={1}>
        <Text dimColor>{session.cwd}</Text>
      </Box>

      {/* Activities timeline */}
      {recentActivities.length > 0 && (
        <Box flexDirection="column" marginTop={1}>
          <Text bold underline>Activities</Text>
          {recentActivities.map((a, i) => {
            const time = new Date(a.timestamp).toLocaleTimeString();
            return (
              <Box key={i}>
                <Text dimColor>{time} </Text>
                <Text>{a.summary}</Text>
              </Box>
            );
          })}
        </Box>
      )}

      {/* Motivation text */}
      {session.last_text && (
        <Box marginTop={1} flexDirection="column">
          <Text bold underline>Last output</Text>
          <Text wrap="truncate-end">
            {session.last_text.slice(0, 200)}
          </Text>
        </Box>
      )}
    </Box>
  );
}

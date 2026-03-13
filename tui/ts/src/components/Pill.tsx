import React from "react";
import { Box, Text } from "ink";
import type { Session } from "../client/client.js";

const STATE_ICON: Record<Session["state"], string> = {
  running: "\u2699",
  waiting: "\u23F3",
  idle: "\u2713",
  dead: "\u25CB",
};

const STATE_COLOR: Record<Session["state"], string> = {
  running: "green",
  waiting: "yellow",
  idle: "gray",
  dead: "blackBright",
};

interface PillProps {
  session: Session;
  selected: boolean;
}

export function Pill({ session, selected }: PillProps) {
  const icon = STATE_ICON[session.state];
  const color = STATE_COLOR[session.state];

  let bgColor: string | undefined;
  if (session.autopilot && session.has_destructive) {
    bgColor = "redBright";
  } else if (session.autopilot) {
    bgColor = "greenBright";
  }

  return (
    <Box
      borderStyle={selected ? "bold" : "single"}
      borderColor={selected ? "cyan" : color}
      paddingLeft={1}
      paddingRight={1}
      marginRight={1}
    >
      <Text
        color={color}
        bold={selected}
        backgroundColor={bgColor}
      >
        {icon} {session.project_name}
      </Text>
    </Box>
  );
}

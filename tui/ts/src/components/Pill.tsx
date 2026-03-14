import React, { useState, useEffect } from "react";
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

function looksLikeCommand(s: string): boolean {
  for (const marker of ["&&", "|", ";", "cd ", "./", "  "]) {
    if (s.includes(marker)) return true;
  }
  if (s.startsWith("/")) return true;
  return false;
}

function pillName(session: Session): string {
  if (session.ghostty_tab && !looksLikeCommand(session.ghostty_tab)) {
    return session.ghostty_tab;
  }
  if (session.slug) return session.slug;
  if (session.project_name) return session.project_name;
  return session.session_id.slice(0, 8);
}

interface PillProps {
  session: Session;
  selected: boolean;
}

export function Pill({ session, selected }: PillProps) {
  const icon = STATE_ICON[session.state];
  const color = STATE_COLOR[session.state];
  const name = pillName(session);

  // Glow sweep animation for running sessions.
  const [glowPos, setGlowPos] = useState(0);
  const [glowDir, setGlowDir] = useState(1);

  useEffect(() => {
    if (session.state !== "running") return;
    const timer = setInterval(() => {
      setGlowPos((pos) => {
        const maxLen = name.length;
        let newPos = pos + glowDir;
        if (newPos >= maxLen) {
          newPos = maxLen - 1;
          setGlowDir(-1);
        }
        if (newPos <= 0) {
          newPos = 0;
          setGlowDir(1);
        }
        return newPos;
      });
    }, 150);
    return () => clearInterval(timer);
  }, [session.state, name.length, glowDir]);

  let bgColor: string | undefined;
  if (selected) {
    bgColor = color;
  } else if (session.autopilot && session.has_destructive) {
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
        color={selected ? "white" : color}
        bold={selected}
        backgroundColor={bgColor}
      >
        {icon} {name}
      </Text>
    </Box>
  );
}

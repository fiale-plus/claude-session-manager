import React from "react";
import { Box } from "ink";
import { Pill } from "./Pill.js";
import type { Session } from "../client/client.js";

interface StripProps {
  sessions: Session[];
  selectedIdx: number;
}

export function Strip({ sessions, selectedIdx }: StripProps) {
  return (
    <Box flexDirection="row" flexWrap="wrap">
      {sessions.map((s, i) => (
        <Pill key={s.session_id} session={s} selected={i === selectedIdx} />
      ))}
    </Box>
  );
}

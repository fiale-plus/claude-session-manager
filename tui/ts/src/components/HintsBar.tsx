import React from "react";
import { Box, Text } from "ink";

interface HintsBarProps {
  queueVisible: boolean;
}

export function HintsBar({ queueVisible }: HintsBarProps) {
  return (
    <Box
      borderStyle="single"
      borderColor="gray"
      paddingLeft={1}
      paddingRight={1}
    >
      <Text dimColor>
        <Text bold color="white">{"\u2190\u2192"}</Text> navigate{"  "}
        <Text bold color="white">Enter</Text> focus{"  "}
        <Text bold color="white">a</Text> autopilot{"  "}
        <Text bold color="white">Q</Text> {queueVisible ? "hide" : "show"} queue{"  "}
        <Text bold color="white">y</Text> approve{"  "}
        <Text bold color="white">n</Text> reject{"  "}
        <Text bold color="white">A</Text> approve all safe{"  "}
        <Text bold color="white">h</Text> help{"  "}
        <Text bold color="white">q</Text> quit
      </Text>
    </Box>
  );
}

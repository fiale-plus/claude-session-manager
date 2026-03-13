#!/bin/bash
# CSM hook — forwards CC hook events to the daemon via Unix socket.
# If the daemon is not running, exits cleanly (passthrough).
input=$(cat)
response=$(echo "$input" | nc -U /tmp/csm.sock 2>/dev/null)
if [ $? -ne 0 ]; then
  exit 0  # Daemon not running — passthrough
fi
echo "$response"

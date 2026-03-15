#!/bin/bash
input=$(cat)
response=$(curl -s --max-time 28 -X POST http://127.0.0.1:19380/hooks \
  -H "Content-Type: application/json" \
  -d "$input" 2>/dev/null)
if [ $? -ne 0 ] || [ -z "$response" ]; then
  exit 0
fi
echo "$response"

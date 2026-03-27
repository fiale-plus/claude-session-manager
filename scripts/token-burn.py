#!/usr/bin/env python3
"""
Claude Code token burn rate analyzer.

Scans ~/.claude/projects/*/*.jsonl session files and aggregates token usage
by day, session, model, and project. Outputs CSV or JSON for dashboarding.

Usage:
    python3 token-burn.py                    # last 7 days, table
    python3 token-burn.py --days 30          # last 30 days
    python3 token-burn.py --csv              # CSV to stdout
    python3 token-burn.py --json             # JSON to stdout
    python3 token-burn.py --by session       # per-session breakdown
    python3 token-burn.py --by project       # per-project breakdown
    python3 token-burn.py --by model         # per-model breakdown
    python3 token-burn.py --by hour          # hourly heatmap
"""

import argparse
import csv
import glob
import json
import os
import sys
from collections import defaultdict
from datetime import datetime, timedelta, timezone

CLAUDE_DIR = os.path.expanduser("~/.claude/projects")
SESSIONS_DIR = os.path.expanduser("~/.claude/sessions")

# Pricing per 1M tokens (as of 2026-03, Opus 4.6 / Sonnet 4.6)
PRICING = {
    "claude-opus-4-6": {"input": 5.0, "output": 25.0, "cache_write": 10.0, "cache_read": 0.50},
    "claude-sonnet-4-6": {"input": 3.0, "output": 15.0, "cache_write": 6.0, "cache_read": 0.30},
    "claude-sonnet-4-5-20250929": {"input": 3.0, "output": 15.0, "cache_write": 6.0, "cache_read": 0.30},
    "claude-haiku-4-5-20251001": {"input": 1.0, "output": 5.0, "cache_write": 2.0, "cache_read": 0.10},
    "MiniMax-M2.7": {"input": 0.30, "output": 1.20, "cache_write": 0.375, "cache_read": 0.06},
}
DEFAULT_PRICING = {"input": 5.0, "output": 25.0, "cache_write": 10.0, "cache_read": 0.50}


def estimate_cost(model, usage):
    """Estimate USD cost from usage dict."""
    p = PRICING.get(model, DEFAULT_PRICING)
    input_tok = usage.get("input_tokens", 0)
    output_tok = usage.get("output_tokens", 0)
    cache_write = usage.get("cache_creation_input_tokens", 0)
    cache_read = usage.get("cache_read_input_tokens", 0)
    cost = (
        input_tok * p["input"] / 1_000_000
        + output_tok * p["output"] / 1_000_000
        + cache_write * p["cache_write"] / 1_000_000
        + cache_read * p["cache_read"] / 1_000_000
    )
    return cost


def load_session_meta():
    """Load session metadata (name, cwd) from sessions dir."""
    meta = {}
    for f in glob.glob(f"{SESSIONS_DIR}/*.json"):
        try:
            with open(f) as fh:
                d = json.load(fh)
                meta[d.get("sessionId", "")] = {
                    "name": d.get("name", ""),
                    "cwd": d.get("cwd", ""),
                    "started": d.get("startedAt", 0),
                }
        except (json.JSONDecodeError, KeyError):
            pass
    return meta


def scan_sessions(since_date):
    """Scan all session JSONL files, yield per-message token records."""
    jsonl_files = glob.glob(f"{CLAUDE_DIR}/**/*.jsonl", recursive=True)

    # Filter by modification time for speed
    cutoff_ts = since_date.timestamp()
    jsonl_files = [f for f in jsonl_files if os.path.getmtime(f) >= cutoff_ts]

    for filepath in jsonl_files:
        project = os.path.basename(os.path.dirname(filepath))
        session_id = os.path.splitext(os.path.basename(filepath))[0]

        try:
            with open(filepath) as fh:
                for line in fh:
                    try:
                        obj = json.loads(line)
                    except json.JSONDecodeError:
                        continue

                    if obj.get("type") != "assistant":
                        continue

                    msg = obj.get("message", {})
                    usage = msg.get("usage")
                    if not usage:
                        continue

                    ts_str = obj.get("timestamp", "")
                    if not ts_str:
                        continue

                    try:
                        ts = datetime.fromisoformat(ts_str.replace("Z", "+00:00"))
                    except (ValueError, AttributeError):
                        continue

                    if ts.date() < since_date.date():
                        continue

                    model = msg.get("model", "unknown")
                    input_tok = usage.get("input_tokens", 0)
                    output_tok = usage.get("output_tokens", 0)
                    cache_write = usage.get("cache_creation_input_tokens", 0)
                    cache_read = usage.get("cache_read_input_tokens", 0)
                    total = input_tok + output_tok + cache_write + cache_read
                    cost = estimate_cost(model, usage)

                    yield {
                        "timestamp": ts,
                        "date": ts.strftime("%Y-%m-%d"),
                        "hour": ts.hour,
                        "session_id": session_id,
                        "project": project,
                        "model": model,
                        "input_tokens": input_tok,
                        "output_tokens": output_tok,
                        "cache_write_tokens": cache_write,
                        "cache_read_tokens": cache_read,
                        "total_tokens": total,
                        "cost_usd": cost,
                    }
        except (IOError, OSError):
            continue


def aggregate(records, group_by="date"):
    """Aggregate records by the given key."""
    buckets = defaultdict(lambda: {
        "input_tokens": 0,
        "output_tokens": 0,
        "cache_write_tokens": 0,
        "cache_read_tokens": 0,
        "total_tokens": 0,
        "cost_usd": 0.0,
        "messages": 0,
        "sessions": set(),
    })

    for r in records:
        if group_by == "hour":
            key = f"{r['hour']:02d}:00"
        else:
            key = r[group_by]

        b = buckets[key]
        b["input_tokens"] += r["input_tokens"]
        b["output_tokens"] += r["output_tokens"]
        b["cache_write_tokens"] += r["cache_write_tokens"]
        b["cache_read_tokens"] += r["cache_read_tokens"]
        b["total_tokens"] += r["total_tokens"]
        b["cost_usd"] += r["cost_usd"]
        b["messages"] += 1
        b["sessions"].add(r["session_id"])

    # Convert sets to counts
    result = {}
    for key, b in sorted(buckets.items()):
        b["session_count"] = len(b.pop("sessions"))
        result[key] = b

    return result


def format_tokens(n):
    if n >= 1_000_000:
        return f"{n / 1_000_000:.1f}M"
    if n >= 1_000:
        return f"{n / 1_000:.1f}K"
    return str(n)


def print_table(agg, group_label="Date"):
    """Print a readable ASCII table."""
    print(f"\n{'─' * 90}")
    print(f" {group_label:<20} {'Input':>8} {'Output':>8} {'CacheW':>8} {'CacheR':>8} {'Total':>9} {'Cost':>8} {'Msgs':>5}")
    print(f"{'─' * 90}")

    grand = defaultdict(float)
    grand["messages"] = 0

    for key, b in agg.items():
        label = key[:20] if len(str(key)) > 20 else key
        print(
            f" {label:<20} "
            f"{format_tokens(b['input_tokens']):>8} "
            f"{format_tokens(b['output_tokens']):>8} "
            f"{format_tokens(b['cache_write_tokens']):>8} "
            f"{format_tokens(b['cache_read_tokens']):>8} "
            f"{format_tokens(b['total_tokens']):>9} "
            f"${b['cost_usd']:>6.2f} "
            f"{b['messages']:>5}"
        )
        for k in ["input_tokens", "output_tokens", "cache_write_tokens", "cache_read_tokens", "total_tokens", "cost_usd"]:
            grand[k] += b[k]
        grand["messages"] += b["messages"]

    print(f"{'─' * 90}")
    print(
        f" {'TOTAL':<20} "
        f"{format_tokens(int(grand['input_tokens'])):>8} "
        f"{format_tokens(int(grand['output_tokens'])):>8} "
        f"{format_tokens(int(grand['cache_write_tokens'])):>8} "
        f"{format_tokens(int(grand['cache_read_tokens'])):>8} "
        f"{format_tokens(int(grand['total_tokens'])):>9} "
        f"${grand['cost_usd']:>6.2f} "
        f"{int(grand['messages']):>5}"
    )
    print(f"{'─' * 90}\n")

    # Burn rate summary
    days = len(agg)
    if days > 0:
        avg_day = grand["cost_usd"] / days
        avg_tok = int(grand["total_tokens"]) / days
        print(f"  Avg/day:  {format_tokens(int(avg_tok))} tokens  |  ${avg_day:.2f}")
        print(f"  Projected/month:  {format_tokens(int(avg_tok * 30))} tokens  |  ${avg_day * 30:.2f}")
        print()


def print_csv(agg, group_label="date"):
    """Print CSV to stdout."""
    writer = csv.writer(sys.stdout)
    writer.writerow([
        group_label, "input_tokens", "output_tokens", "cache_write_tokens",
        "cache_read_tokens", "total_tokens", "cost_usd", "messages", "session_count"
    ])
    for key, b in agg.items():
        writer.writerow([
            key, b["input_tokens"], b["output_tokens"], b["cache_write_tokens"],
            b["cache_read_tokens"], b["total_tokens"], f"{b['cost_usd']:.4f}",
            b["messages"], b["session_count"]
        ])


def print_json(agg, group_label="date"):
    """Print JSON to stdout."""
    out = []
    for key, b in agg.items():
        row = {group_label: key}
        row.update(b)
        row["cost_usd"] = round(row["cost_usd"], 4)
        out.append(row)
    json.dump(out, sys.stdout, indent=2, default=str)
    print()


def main():
    parser = argparse.ArgumentParser(description="Claude Code token burn rate analyzer")
    parser.add_argument("--days", type=int, default=7, help="Look back N days (default: 7)")
    parser.add_argument("--since", type=str, help="Start date YYYY-MM-DD (overrides --days)")
    parser.add_argument("--by", choices=["date", "session", "project", "model", "hour"],
                        default="date", help="Group by (default: date)")
    parser.add_argument("--csv", action="store_true", help="Output CSV")
    parser.add_argument("--json", action="store_true", help="Output JSON")
    args = parser.parse_args()

    if args.since:
        since = datetime.strptime(args.since, "%Y-%m-%d").replace(tzinfo=timezone.utc)
    else:
        since = datetime.now(timezone.utc) - timedelta(days=args.days)

    group_map = {
        "date": "date",
        "session": "session_id",
        "project": "project",
        "model": "model",
        "hour": "hour",
    }

    sys.stderr.write(f"Scanning sessions since {since.strftime('%Y-%m-%d')}...\n")
    records = list(scan_sessions(since))
    sys.stderr.write(f"Found {len(records)} assistant messages with token data\n")

    if not records:
        sys.stderr.write("No data found.\n")
        return

    agg = aggregate(records, group_by=group_map[args.by])

    label_map = {"date": "Date", "session": "Session", "project": "Project", "model": "Model", "hour": "Hour"}

    if args.csv:
        print_csv(agg, args.by)
    elif args.json:
        print_json(agg, args.by)
    else:
        print_table(agg, label_map[args.by])


if __name__ == "__main__":
    main()

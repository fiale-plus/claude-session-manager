// Package scanner provides fallback session discovery via process table
// and JSONL file parsing, for sessions started before the daemon.
package scanner

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/pchaganti/claude-session-manager/daemon/internal/model"
)

// jsonlEntry represents a parsed line from a CC JSONL session log.
type jsonlEntry struct {
	Type      string    `json:"type"`
	Subtype   string    `json:"subtype,omitempty"`
	Timestamp string    `json:"timestamp,omitempty"`
	SessionID string    `json:"sessionId,omitempty"`
	Slug      string    `json:"slug,omitempty"`
	CWD       string    `json:"cwd,omitempty"`
	GitBranch string    `json:"gitBranch,omitempty"`
	Message   *message  `json:"message,omitempty"`
}

type message struct {
	Content json.RawMessage `json:"content"`
}

type contentBlock struct {
	Type      string         `json:"type"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Text      string         `json:"text,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
}

// ReadTail reads the last n lines from a JSONL file.
func ReadTail(path string, n int) ([]jsonlEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	if info.Size() < 64*1024 {
		return readAllLines(f, n)
	}
	return readTailSeek(f, n, info.Size())
}

func readAllLines(r io.Reader, n int) ([]jsonlEntry, error) {
	var entries []jsonlEntry
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var e jsonlEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	if len(entries) > n {
		entries = entries[len(entries)-n:]
	}
	return entries, nil
}

func readTailSeek(f *os.File, n int, size int64) ([]jsonlEntry, error) {
	chunkSize := int64(8192)
	if chunkSize > size {
		chunkSize = size
	}

	var lines [][]byte
	offset := size
	var leftover []byte

	for len(lines) < n+1 && offset > 0 {
		readSize := chunkSize
		if readSize > offset {
			readSize = offset
		}
		offset -= readSize

		buf := make([]byte, readSize)
		if _, err := f.ReadAt(buf, offset); err != nil && err != io.EOF {
			return nil, err
		}

		buf = append(buf, leftover...)
		parts := splitLines(buf)
		leftover = parts[0]
		for i := 1; i < len(parts); i++ {
			if len(parts[i]) > 0 {
				lines = append([][]byte{parts[i]}, lines...)
			}
		}

		if chunkSize < 1024*1024 {
			chunkSize *= 2
		}
	}

	if len(leftover) > 0 {
		lines = append([][]byte{leftover}, lines...)
	}

	// Parse last n lines.
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	var entries []jsonlEntry
	for _, line := range lines {
		var e jsonlEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func splitLines(data []byte) [][]byte {
	var result [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			result = append(result, data[start:i])
			start = i + 1
		}
	}
	result = append(result, data[start:])
	return result
}

// DetectState determines session state from JSONL entries.
func DetectState(entries []jsonlEntry) model.SessionState {
	meaningful := filterMeaningful(entries)
	if len(meaningful) == 0 {
		return model.StateIdle
	}

	last := meaningful[len(meaningful)-1]

	// System entries that indicate the turn ended → waiting.
	if last.Type == "system" {
		switch last.Subtype {
		case "turn_duration", "stop_hook_summary", "local_command":
			return model.StateWaiting
		}
	}

	// Non-standard entry types at the end (custom-title, agent-name, etc.)
	// mean the session is idle, not running.
	if last.Type != "user" && last.Type != "assistant" && last.Type != "system" {
		return model.StateIdle
	}

	// Walk backward looking for pending tool_use.
	for i := len(meaningful) - 1; i >= 0; i-- {
		entry := meaningful[i]

		if entry.Type == "assistant" && hasToolUse(entry) {
			toolIDs := getToolUseIDs(entry)
			hasResult := false
			for _, later := range meaningful[i+1:] {
				if isToolResult(later) {
					for _, id := range getToolResultIDs(later) {
						if _, ok := toolIDs[id]; ok {
							hasResult = true
							break
						}
					}
				}
				if hasResult {
					break
				}
			}
			if !hasResult {
				return model.StateRunning
			}
			continue
		}

		if entry.Type == "user" && !isToolResult(entry) {
			if i == len(meaningful)-1 {
				return model.StateRunning
			}
			break
		}

		if entry.Type == "assistant" {
			if i == len(meaningful)-1 {
				return model.StateWaiting
			}
			break
		}
	}

	return model.StateIdle
}

// ExtractMotivation returns the last assistant text block.
func ExtractMotivation(entries []jsonlEntry) string {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Type != "assistant" {
			continue
		}
		blocks := parseContent(entries[i])
		for j := len(blocks) - 1; j >= 0; j-- {
			if blocks[j].Type == "text" && blocks[j].Text != "" {
				text := blocks[j].Text
				if len(text) > 500 {
					text = text[:500]
				}
				return text
			}
		}
	}
	return ""
}

// ExtractActivities parses recent entries into activities.
func ExtractActivities(entries []jsonlEntry, n int) []model.Activity {
	var activities []model.Activity
	meaningful := filterMeaningful(entries)

	for _, entry := range meaningful {
		ts := parseTimestamp(entry.Timestamp)
		if ts.IsZero() {
			continue
		}

		switch entry.Type {
		case "assistant":
			for _, block := range parseContent(entry) {
				switch block.Type {
				case "tool_use":
					brief := summarizeToolInput(block.Name, block.Input)
					activities = append(activities, model.Activity{
						Timestamp:    ts,
						ActivityType: model.ActivityToolUse,
						Summary:      block.Name + ": " + brief,
					})
				case "text":
					summary := block.Text
					if len(summary) > 60 {
						summary = summary[:60]
					}
					activities = append(activities, model.Activity{
						Timestamp:    ts,
						ActivityType: model.ActivityText,
						Summary:      summary,
					})
				}
			}
		case "user":
			if !isToolResult(entry) {
				summary := extractUserText(entry)
				if len(summary) > 60 {
					summary = summary[:60]
				}
				activities = append(activities, model.Activity{
					Timestamp:    ts,
					ActivityType: model.ActivityUserMessage,
					Summary:      summary,
				})
			}
		case "system":
			activities = append(activities, model.Activity{
				Timestamp:    ts,
				ActivityType: model.ActivitySystem,
				Summary:      entry.Subtype,
			})
		}
	}

	if len(activities) > n {
		activities = activities[len(activities)-n:]
	}
	return activities
}

// ExtractPendingTools finds tool_use blocks from the LAST assistant message
// that don't have matching tool_results after it. Only looks at the final
// assistant entry to avoid false positives from the tail window.
func ExtractPendingTools(entries []jsonlEntry) []model.PendingTool {
	meaningful := filterMeaningful(entries)

	// Find the last assistant entry with tool_use blocks.
	lastAssistantIdx := -1
	for i := len(meaningful) - 1; i >= 0; i-- {
		if meaningful[i].Type == "assistant" && hasToolUse(meaningful[i]) {
			lastAssistantIdx = i
			break
		}
		// If we hit a system entry (turn_duration, stop_hook_summary) before
		// finding an assistant with tool_use, the turn is done — no pending tools.
		if meaningful[i].Type == "system" {
			return nil
		}
	}
	if lastAssistantIdx < 0 {
		return nil
	}

	// Collect tool_use IDs from that last assistant message.
	pending := make(map[string]model.PendingTool)
	for _, block := range parseContent(meaningful[lastAssistantIdx]) {
		if block.Type == "tool_use" && block.ID != "" {
			pending[block.ID] = model.PendingTool{
				ToolName:  block.Name,
				ToolInput: block.Input,
			}
		}
	}

	// Remove any that have tool_results after them.
	for _, entry := range meaningful[lastAssistantIdx+1:] {
		if entry.Type == "user" && isToolResult(entry) {
			for _, id := range getToolResultIDs(entry) {
				delete(pending, id)
			}
		}
	}

	result := make([]model.PendingTool, 0, len(pending))
	for _, t := range pending {
		result = append(result, t)
	}
	return result
}

// ExtractMetadata extracts sessionId, slug, cwd, gitBranch from entries.
func ExtractMetadata(entries []jsonlEntry) (sessionID, slug, cwd, gitBranch string) {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if sessionID == "" && e.SessionID != "" {
			sessionID = e.SessionID
		}
		if slug == "" && e.Slug != "" {
			slug = e.Slug
		}
		if cwd == "" && e.CWD != "" {
			cwd = e.CWD
		}
		if gitBranch == "" && e.GitBranch != "" {
			gitBranch = e.GitBranch
		}
		if sessionID != "" && slug != "" && cwd != "" && gitBranch != "" {
			break
		}
	}
	return
}

// --- helpers ---

func filterMeaningful(entries []jsonlEntry) []jsonlEntry {
	var result []jsonlEntry
	for _, e := range entries {
		if e.Type == "user" || e.Type == "assistant" || e.Type == "system" {
			result = append(result, e)
		}
	}
	return result
}

func parseContent(entry jsonlEntry) []contentBlock {
	if entry.Message == nil {
		return nil
	}
	var blocks []contentBlock
	if err := json.Unmarshal(entry.Message.Content, &blocks); err != nil {
		return nil
	}
	return blocks
}

func hasToolUse(entry jsonlEntry) bool {
	for _, b := range parseContent(entry) {
		if b.Type == "tool_use" {
			return true
		}
	}
	return false
}

func isToolResult(entry jsonlEntry) bool {
	if entry.Type != "user" {
		return false
	}
	for _, b := range parseContent(entry) {
		if b.Type == "tool_result" {
			return true
		}
	}
	return false
}

func getToolUseIDs(entry jsonlEntry) map[string]struct{} {
	ids := make(map[string]struct{})
	for _, b := range parseContent(entry) {
		if b.Type == "tool_use" && b.ID != "" {
			ids[b.ID] = struct{}{}
		}
	}
	return ids
}

func getToolResultIDs(entry jsonlEntry) []string {
	var ids []string
	for _, b := range parseContent(entry) {
		if b.Type == "tool_result" && b.ToolUseID != "" {
			ids = append(ids, b.ToolUseID)
		}
	}
	return ids
}

func extractUserText(entry jsonlEntry) string {
	if entry.Message == nil {
		return ""
	}
	// Try as string first.
	var s string
	if err := json.Unmarshal(entry.Message.Content, &s); err == nil {
		return s
	}
	// Try as array of blocks.
	blocks := parseContent(entry)
	var texts []string
	for _, b := range blocks {
		if b.Text != "" {
			texts = append(texts, b.Text)
		}
	}
	if len(texts) > 0 {
		result := ""
		for _, t := range texts {
			if result != "" {
				result += " "
			}
			result += t
		}
		return result
	}
	return ""
}

func summarizeToolInput(toolName string, input map[string]any) string {
	if input == nil {
		return ""
	}
	for _, key := range []string{"command", "file_path", "pattern", "query", "description"} {
		if v, ok := input[key]; ok {
			s := toString(v)
			if len(s) > 60 {
				s = s[:60]
			}
			return s
		}
	}
	for k, v := range input {
		s := toString(v)
		if len(s) > 30 {
			s = s[:30]
		}
		return k + "=" + s
	}
	return ""
}

func toString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	default:
		b, _ := json.Marshal(val)
		return string(b)
	}
}

func parseTimestamp(ts string) time.Time {
	if ts == "" {
		return time.Time{}
	}
	// Handle Z suffix.
	if len(ts) > 0 && ts[len(ts)-1] == 'Z' {
		ts = ts[:len(ts)-1] + "+00:00"
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		// Try RFC3339Nano.
		t, err = time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return time.Time{}
		}
	}
	return t
}

package forge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// LogEntry represents a single parsed entry from a worker's stream-json log.
// Type is one of "tool_use", "text", or "think". Note: tool_result events from
// the stream-json log are NOT emitted as separate entries; they are correlated
// back onto the matching tool_use entry (Status and Content are updated in place).
type LogEntry struct {
	Type    string `json:"type"`    // "tool_use", "text", "think"
	Name    string `json:"name"`    // tool name for tool_use entries
	Content string `json:"content"` // text content, formatted tool input, or result summary
	Status  string `json:"status"`  // "success" or "error" for tool_use; empty until result arrives
}

// rawLogLine is the top-level JSON object for each line in a stream-json log.
type rawLogLine struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
}

// rawMessage holds the content array from assistant or user messages.
type rawMessage struct {
	Content []rawContentBlock `json:"content"`
}

// rawContentBlock is a single content item inside a message.
type rawContentBlock struct {
	// Common
	Type string `json:"type"`
	// thinking blocks
	Thinking string `json:"thinking"`
	// text blocks
	Text string `json:"text"`
	// tool_use blocks
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
	// tool_result blocks
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"` // string or []content-block
	IsError   bool            `json:"is_error"`
}

// ParseWorkerLog reads a worker's stream-json log file line by line and returns
// a slice of structured LogEntry values. Tool results are correlated back to
// their matching tool_use entries by tool_use_id: the Status field is set to
// "success" or "error" and the Content is enriched with a truncated result summary.
func ParseWorkerLog(logFilePath string) ([]LogEntry, error) {
	f, err := os.Open(logFilePath) //nolint:gosec
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []LogEntry
	// toolUseIndex maps tool_use_id → index in entries for O(1) correlation.
	toolUseIndex := make(map[string]int)

	scanner := bufio.NewScanner(f)
	// Increase the buffer for long lines (tool outputs can be large).
	const maxLineSize = 10 * 1024 * 1024 // 10 MiB
	scanner.Buffer(make([]byte, 64*1024), maxLineSize)
	// Limit parsed entries to avoid unbounded memory usage on large log files.
	const maxEntries = 5000

	for scanner.Scan() {
		if len(entries) >= maxEntries {
			break
		}
		line := scanner.Text()
		if line == "" {
			continue
		}

		var raw rawLogLine
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}

		switch raw.Type {
		case "assistant":
			var msg rawMessage
			if err := json.Unmarshal(raw.Message, &msg); err != nil {
				continue
			}
			for _, block := range msg.Content {
				switch block.Type {
				case "thinking":
					entries = append(entries, LogEntry{
						Type:    "think",
						Content: block.Thinking,
					})
				case "text":
					if strings.TrimSpace(block.Text) != "" {
						entries = append(entries, LogEntry{
							Type:    "text",
							Content: block.Text,
						})
					}
				case "tool_use":
					idx := len(entries)
					entries = append(entries, LogEntry{
						Type:    "tool_use",
						Name:    block.Name,
						Content: formatToolInput(block.Name, block.Input),
						// Status is left empty until a tool_result is correlated;
						// this avoids misreporting failures as success when the log
						// is read mid-run or when a tool_result is missing.
					})
					if block.ID != "" {
						toolUseIndex[block.ID] = idx
					}
				}
			}

		case "user":
			var msg rawMessage
			if err := json.Unmarshal(raw.Message, &msg); err != nil {
				continue
			}
			for _, block := range msg.Content {
				if block.Type != "tool_result" {
					continue
				}
				idx, ok := toolUseIndex[block.ToolUseID]
				if !ok {
					continue
				}
				if block.IsError {
					entries[idx].Status = "error"
				} else {
					entries[idx].Status = "success"
				}
				// Enrich the tool_use entry with a truncated result summary.
				result := extractResultContent(block.Content)
				if result != "" {
					entries[idx].Content = fmt.Sprintf("%s\n\n%s", entries[idx].Content, truncateString(result, 500))
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

// formatToolInput renders a tool's JSON input as a concise human-readable string.
func formatToolInput(name string, input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal(input, &m); err != nil {
		return string(input)
	}
	switch name {
	case "Bash":
		if cmd, ok := m["command"].(string); ok {
			if desc, ok := m["description"].(string); ok && desc != "" {
				return desc + "\n$ " + cmd
			}
			return "$ " + cmd
		}
	case "Read":
		if p, ok := m["file_path"].(string); ok {
			return p
		}
	case "Write":
		if p, ok := m["file_path"].(string); ok {
			return p
		}
	case "Edit":
		if p, ok := m["file_path"].(string); ok {
			return p
		}
	case "Grep":
		if pat, ok := m["pattern"].(string); ok {
			return fmt.Sprintf("pattern=%q", pat)
		}
	case "Glob":
		if pat, ok := m["pattern"].(string); ok {
			return fmt.Sprintf("pattern=%q", pat)
		}
	}
	// Fallback: compact JSON.
	out, _ := json.Marshal(m)
	return string(out)
}

// extractResultContent converts a tool_result content field to a plain string.
// The content may be a JSON string or a JSON array of content blocks.
func extractResultContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return string(raw)
}

// truncateString returns s truncated to at most n runes, appending "..." if truncated.
func truncateString(s string, n int) string {
	if n <= 0 {
		if len(s) == 0 {
			return ""
		}
		return "..."
	}
	runeCount := 0
	for i := range s {
		if runeCount == n {
			// i is the byte index of the start of the (n+1)th rune; truncate before it.
			return s[:i] + "..."
		}
		runeCount++
	}
	// String has n or fewer runes; no truncation needed.
	return s
}

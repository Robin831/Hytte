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
	Seq     int    `json:"seq"`     // zero-based index of this entry in the full log; stable across tail truncation
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
	// toolUseIndex maps tool_use_id → seq number for O(1) correlation.
	toolUseIndex := make(map[string]int)
	// seqCounter is a monotonic counter that provides stable Seq values
	// across rolling-buffer compactions.
	seqCounter := 0

	scanner := bufio.NewScanner(f)
	// Increase the buffer for long lines (tool outputs can be large).
	const maxLineSize = 10 * 1024 * 1024 // 10 MiB
	scanner.Buffer(make([]byte, 64*1024), maxLineSize)
	// Maximum entries retained in the result. A rolling buffer keeps memory
	// bounded while scanning to EOF so that ?tail=N refers to the true tail.
	const maxEntries = 5000
	// compactThreshold triggers a compaction that copies the last maxEntries
	// into a fresh slice, allowing the GC to reclaim evicted entries.
	const compactThreshold = maxEntries + maxEntries/2

	// compactEntries trims the entries slice to the last maxEntries and
	// removes stale toolUseIndex references that point to evicted entries.
	compactEntries := func() {
		if len(entries) < compactThreshold {
			return
		}
		keep := make([]LogEntry, maxEntries)
		copy(keep, entries[len(entries)-maxEntries:])
		entries = keep
		firstSeq := entries[0].Seq
		for id, seq := range toolUseIndex {
			if seq < firstSeq {
				delete(toolUseIndex, id)
			}
		}
	}

	// lookupEntry finds an entry by its Seq number in the rolling buffer.
	// Returns the slice index and true if found, or -1 and false if evicted.
	lookupEntry := func(seq int) (int, bool) {
		if len(entries) == 0 {
			return -1, false
		}
		firstSeq := entries[0].Seq
		pos := seq - firstSeq
		if pos < 0 || pos >= len(entries) {
			return -1, false
		}
		return pos, true
	}

	for scanner.Scan() {
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
						Seq:     seqCounter,
						Type:    "think",
						Content: block.Thinking,
					})
					seqCounter++
				case "text":
					if strings.TrimSpace(block.Text) != "" {
						entries = append(entries, LogEntry{
							Seq:     seqCounter,
							Type:    "text",
							Content: block.Text,
						})
						seqCounter++
					}
				case "tool_use":
					entries = append(entries, LogEntry{
						Seq:     seqCounter,
						Type:    "tool_use",
						Name:    block.Name,
						Content: formatToolInput(block.Name, block.Input),
					})
					if block.ID != "" {
						toolUseIndex[block.ID] = seqCounter
					}
					seqCounter++
				}
				compactEntries()
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
				targetSeq, ok := toolUseIndex[block.ToolUseID]
				if !ok {
					continue
				}
				// Remove from index once correlated — the result has been
				// processed so the ID is no longer needed.
				delete(toolUseIndex, block.ToolUseID)
				pos, found := lookupEntry(targetSeq)
				if !found {
					continue // entry was evicted from rolling buffer
				}
				if block.IsError {
					entries[pos].Status = "error"
				} else {
					entries[pos].Status = "success"
				}
				// Enrich the tool_use entry with a truncated result summary.
				result := extractResultContent(block.Content)
				if result != "" {
					entries[pos].Content = fmt.Sprintf("%s\n\n%s", entries[pos].Content, truncateString(result, 500))
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

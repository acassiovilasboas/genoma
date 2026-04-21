package sandbox

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"
)

// ProtocolMessage represents a message in the sandbox communication protocol.
// Communication uses JSON-lines (one JSON object per line) over stdin/stdout.
type ProtocolMessage struct {
	Type      string `json:"type"`                 // "result", "error", "log"
	Data      any    `json:"data,omitempty"`        // Payload
	Traceback string `json:"traceback,omitempty"`   // Stack trace (errors only)
}

// ProtocolInput is the input sent to the sandbox wrapper via stdin.
type ProtocolInput struct {
	Input      map[string]any `json:"input"`
	ScriptPath string         `json:"script_path"`
}

// ParseOutput parses the sandbox stdout (JSON-lines) into structured results.
func ParseOutput(output string) (result map[string]any, logs []string, execErr string) {
	logs = make([]string, 0)
	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var msg ProtocolMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			// Non-JSON lines are treated as raw log output
			logs = append(logs, line)
			continue
		}

		switch msg.Type {
		case "result":
			if data, ok := msg.Data.(map[string]any); ok {
				result = data
			} else {
				result = map[string]any{"value": msg.Data}
			}
		case "error":
			if errStr, ok := msg.Data.(string); ok {
				execErr = errStr
			} else {
				execErr = fmt.Sprintf("%v", msg.Data)
			}
			if msg.Traceback != "" {
				execErr += "\n" + msg.Traceback
			}
		case "log":
			if logStr, ok := msg.Data.(string); ok {
				logs = append(logs, logStr)
			}
		}
	}

	return result, logs, execErr
}

// FormatInput creates the stdin payload for the sandbox wrapper.
func FormatInput(input map[string]any, scriptPath string) ([]byte, error) {
	return json.Marshal(ProtocolInput{
		Input:      input,
		ScriptPath: scriptPath,
	})
}

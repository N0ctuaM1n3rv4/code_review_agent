package trajectory

import (
	"encoding/json"
	"fmt"
	"strings"

	"code-review-agent/internal/llm"
)

func (t *Trajectory) ToMessages(systemPrompt, initialUserMsg string) []llm.Message {
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: systemPrompt},
		{Role: llm.RoleUser, Content: initialUserMsg},
	}
	for _, step := range t.Steps {
		if step.FromSummary {
			msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Content: step.Observation})
			continue
		}
		if step.Action.Type == "" {
			if step.Observation != "" {
				msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: step.Observation})
			}
			continue
		}
		msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Content: FormatAssistantMsg(step.Thought, step.Action)})
		msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: fmt.Sprintf("Tool result for %s:\n%s", step.Action.Type, step.Observation)})
	}
	return msgs
}

func FormatAssistantMsg(thought string, action Action) string {
	callJSON, _ := json.Marshal(toolCallPayload{Name: action.Type, Arguments: action.Params})
	var b strings.Builder
	if thought != "" {
		b.WriteString("<think>")
		b.WriteString(thought)
		b.WriteString("</think>\n")
	}
	b.WriteString("<tool_call>")
	b.WriteString(string(callJSON))
	b.WriteString("</tool_call>")
	return b.String()
}

type toolCallPayload struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func ParseAssistantMsg(content string) (thought string, actionType string, actionParams json.RawMessage, ok bool) {
	thinkStart := strings.Index(content, "<think>")
	thinkEnd := strings.Index(content, "</think>")
	if thinkStart >= 0 && thinkEnd > thinkStart {
		thought = content[thinkStart+len("<think>") : thinkEnd]
	}
	call, found := parseToolCallJSON(content)
	if !found {
		return thought, "", nil, false
	}
	return thought, call.Name, call.Arguments, true
}

func parseToolCallJSON(text string) (toolCallPayload, bool) {
	open, close := "<tool_call>", "</tool_call>"
	start := strings.Index(text, open)
	if start < 0 {
		return toolCallPayload{}, false
	}
	start += len(open)
	end := strings.Index(text[start:], close)
	if end < 0 {
		return toolCallPayload{}, false
	}
	payload := strings.TrimSpace(text[start : start+end])
	var call toolCallPayload
	if err := json.Unmarshal([]byte(payload), &call); err != nil || call.Name == "" {
		return toolCallPayload{}, false
	}
	return call, true
}

func StepCount(t *Trajectory) int {
	count := 0
	for _, s := range t.Steps {
		if s.Action.Type != "" && !s.FromSummary {
			count++
		}
	}
	return count
}

func HasEndAudit(t *Trajectory) bool {
	for _, s := range t.Steps {
		if s.Action.Type == "end_audit" {
			return true
		}
	}
	return false
}

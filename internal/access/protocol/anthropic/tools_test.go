package anthropic

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/F31/liteAIG/internal/kernel/interaction"
)

func TestDecodeMessagesWithTools(t *testing.T) {
	body := []byte(`{
		"model": "claude-3-5",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "What is the weather in Paris?"},
			{"role": "assistant", "content": [{"type": "text", "text": "Let me check."}, {"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": {"city": "Paris"}}]},
			{"role": "user", "content": [{"type": "tool_result", "tool_use_id": "toolu_1", "content": "sunny, 20C"}]}
		],
		"tools": [{"name": "get_weather", "description": "Weather", "input_schema": {"type": "object"}}],
		"tool_choice": {"type": "tool", "name": "get_weather"}
	}`)
	request, err := DecodeMessagesBytes(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Chat.Tools) != 1 || request.Chat.Tools[0].Function.Name != "get_weather" || string(request.Chat.Tools[0].Function.Parameters) != `{"type": "object"}` {
		t.Fatalf("tools=%+v", request.Chat.Tools)
	}
	if string(request.Chat.ToolChoice) != `{"type": "tool", "name": "get_weather"}` {
		t.Fatalf("tool_choice=%s", request.Chat.ToolChoice)
	}
	// The tool_result block must become its own role:"tool" message.
	var toolMsg *interaction.Message
	var assistant *interaction.Message
	for i := range request.Chat.Messages {
		switch request.Chat.Messages[i].Role {
		case "tool":
			toolMsg = &request.Chat.Messages[i]
		case "assistant":
			assistant = &request.Chat.Messages[i]
		}
	}
	if toolMsg == nil || toolMsg.ToolCallID != "toolu_1" || toolMsg.Content != "sunny, 20C" {
		t.Fatalf("tool message=%+v", toolMsg)
	}
	if assistant == nil || assistant.Content != "Let me check." || len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].ID != "toolu_1" || assistant.ToolCalls[0].Function.Name != "get_weather" {
		t.Fatalf("assistant=%+v", assistant)
	}
}

func TestEncodeMessagesWithTools(t *testing.T) {
	request := &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "claude-3-5", Chat: &interaction.ChatPayload{
		Messages: []interaction.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "checking", ToolCalls: []interaction.ToolCall{{ID: "toolu_9", Type: "function", Function: interaction.ToolCallFunction{Name: "get_weather", Arguments: `{"city":"Paris"}`}}}},
			{Role: "tool", ToolCallID: "toolu_9", Content: "sunny"},
		},
		MaxOutputTokens: intPtr(1024),
		Tools:           []interaction.Tool{{Type: "function", Function: interaction.ToolFunction{Name: "get_weather", Description: "Weather", Parameters: json.RawMessage(`{"type":"object"}`)}}},
		ToolChoice:      json.RawMessage(`"required"`),
	}}
	encoded, err := EncodeMessages(request)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	tools := wire["tools"].([]any)
	tool := tools[0].(map[string]any)
	if tool["name"] != "get_weather" {
		t.Fatalf("tools=%v", tools)
	}
	schema := tool["input_schema"].(map[string]any)
	if schema["type"] != "object" {
		t.Fatalf("input_schema=%v", schema)
	}
	if wire["tool_choice"] != "any" {
		t.Fatalf("required must map to any: %v", wire["tool_choice"])
	}
	messages := wire["messages"].([]any)
	assistant := messages[1].(map[string]any)
	blocks := assistant["content"].([]any)
	if len(blocks) != 2 {
		t.Fatalf("assistant blocks=%v", blocks)
	}
	if blocks[0].(map[string]any)["type"] != "text" || blocks[1].(map[string]any)["type"] != "tool_use" {
		t.Fatalf("blocks=%v", blocks)
	}
	if blocks[1].(map[string]any)["id"] != "toolu_9" {
		t.Fatalf("tool_use=%v", blocks[1])
	}
	result := messages[2].(map[string]any)
	if result["role"] != "user" {
		t.Fatalf("tool result must become a user message: %v", result)
	}
	resultBlocks := result["content"].([]any)
	if resultBlocks[0].(map[string]any)["type"] != "tool_result" || resultBlocks[0].(map[string]any)["tool_use_id"] != "toolu_9" {
		t.Fatalf("tool_result=%v", resultBlocks)
	}
}

func TestDecodeResponseWithToolUse(t *testing.T) {
	body := []byte(`{"id":"msg_1","model":"claude-3-5","content":[{"type":"text","text":"Checking."},{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"city":"Paris"}}],"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":20}}`)
	response, err := DecodeResponse(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if response.StopReason != "tool_use" {
		t.Fatalf("stop=%s", response.StopReason)
	}
	msg := response.Choices[0].Message
	if msg.Content != "Checking." || len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "toolu_1" || msg.ToolCalls[0].Function.Name != "get_weather" || msg.ToolCalls[0].Function.Arguments != `{"city":"Paris"}` {
		t.Fatalf("msg=%+v", msg)
	}
}

func TestEncodeResponseWithToolUse(t *testing.T) {
	response := &interaction.UnifiedResponse{ID: "msg_1", Model: "claude-3-5", StopReason: "tool_use", Usage: interaction.UnifiedUsage{InputTokens: 10, OutputTokens: 20}, Choices: []interaction.Choice{{Index: 0, Message: interaction.Message{Role: "assistant", Content: "Checking.", ToolCalls: []interaction.ToolCall{{ID: "toolu_1", Type: "function", Function: interaction.ToolCallFunction{Name: "get_weather", Arguments: `{"city":"Paris"}`}}}}}}}
	encoded, err := EncodeResponse(response, "claude-3-5")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"stop_reason":"tool_use"`) || !strings.Contains(string(encoded), `"tool_use"`) || !strings.Contains(string(encoded), `"toolu_1"`) {
		t.Fatalf("encoded=%s", encoded)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	blocks := wire["content"].([]any)
	if len(blocks) != 2 || blocks[0].(map[string]any)["type"] != "text" || blocks[1].(map[string]any)["type"] != "tool_use" {
		t.Fatalf("blocks=%v", blocks)
	}
}

func TestStreamToolUseFrames(t *testing.T) {
	start, err := NormalizeStream([]byte(`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{}}}`))
	if err != nil || len(start.ToolCallDeltas) != 1 || start.ToolCallDeltas[0].ID != "toolu_1" || start.ToolCallDeltas[0].Name != "get_weather" || start.ToolCallDeltas[0].Index != 1 {
		t.Fatalf("start=%+v err=%v", start, err)
	}
	frag, err := NormalizeStream([]byte(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"ci"}}`))
	if err != nil || len(frag.ToolCallDeltas) != 1 || frag.ToolCallDeltas[0].Arguments != `{"ci` || frag.Delta != "" {
		t.Fatalf("frag=%+v err=%v", frag, err)
	}

	enc := NewStreamEncoder("claude-3-5")
	var frames []string
	collect := func(event interaction.StreamEvent) {
		out, err := enc.Write(event)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range out {
			frames = append(frames, f.Name+"|"+string(f.Data))
		}
	}
	text := interaction.StreamEvent{Delta: "Checking."}
	collect(text)
	collect(start)
	collect(frag)
	collect(interaction.StreamEvent{Delta: `t:"Paris"}`, Final: false})
	collect(interaction.StreamEvent{StopReason: "tool_use", Final: true})

	joined := strings.Join(frames, "\n")
	for _, want := range []string{
		`"id":"toolu_1"`, `"name":"get_weather"`, `"type":"tool_use"`,
		`"type":"input_json_delta"`, `"partial_json":"{\"ci"`,
		`"stop_reason":"tool_use"`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("frames missing %s:\n%s", want, joined)
		}
	}
	// Content blocks are sequential: the tool block must close before the
	// trailing text block opens, and before message_stop.
	toolStart := strings.Index(joined, `"id":"toolu_1"`)
	toolStop := strings.Index(joined, `{"index":1,"type":"content_block_stop"}`)
	textOpen := strings.Index(joined, `{"content_block":{"text":"","type":"text"},"index":2`)
	endIdx := strings.Index(joined, `"type":"message_stop"`)
	if toolStart == -1 || toolStop == -1 || textOpen == -1 || endIdx == -1 || toolStop > textOpen || textOpen > endIdx {
		t.Fatalf("unexpected frame order:\n%s", joined)
	}
}

func intPtr(v int) *int { return &v }

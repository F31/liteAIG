package openai

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/F31/liteAIG/internal/kernel/interaction"
)

func TestDecodeChatWithTools(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "user", "content": "What is the weather in Paris?"},
			{"role": "assistant", "content": "", "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "get_weather", "arguments": "{\"city\":\"Paris\"}"}}]},
			{"role": "tool", "tool_call_id": "call_1", "content": "sunny, 20C"}
		],
		"tools": [{"type": "function", "function": {"name": "get_weather", "description": "Weather", "parameters": {"type": "object"}}}],
		"tool_choice": "auto",
		"response_format": {"type": "json_object"}
	}`)
	request, err := DecodeChatBytes(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Chat.Tools) != 1 || request.Chat.Tools[0].Function.Name != "get_weather" {
		t.Fatalf("tools=%+v", request.Chat.Tools)
	}
	if string(request.Chat.ToolChoice) != `"auto"` || string(request.Chat.ResponseFormat) != `{"type": "json_object"}` {
		t.Fatalf("tool_choice=%s response_format=%s", request.Chat.ToolChoice, request.Chat.ResponseFormat)
	}
	assistant := request.Chat.Messages[1]
	if assistant.Role != "assistant" || len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].ID != "call_1" || assistant.ToolCalls[0].Function.Name != "get_weather" || assistant.ToolCalls[0].Function.Arguments != `{"city":"Paris"}` {
		t.Fatalf("assistant=%+v", assistant)
	}
	tool := request.Chat.Messages[2]
	if tool.Role != "tool" || tool.ToolCallID != "call_1" || tool.Content != "sunny, 20C" {
		t.Fatalf("tool=%+v", tool)
	}
	if request.ToolCalls() != true {
		t.Fatalf("request with tools must be flagged for the cache gate")
	}
	// tools/tool_choice/response_format must not leak into passthrough params.
	if len(request.Parameters) != 0 {
		t.Fatalf("params=%v", request.Parameters)
	}
}

func TestEncodeChatWithToolsRoundTrip(t *testing.T) {
	request := &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "gpt-4o", Chat: &interaction.ChatPayload{
		Messages: []interaction.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "", ToolCalls: []interaction.ToolCall{{ID: "call_9", Type: "function", Function: interaction.ToolCallFunction{Name: "search", Arguments: `{"q":"a"}`}}}},
			{Role: "tool", ToolCallID: "call_9", Content: "result"},
		},
		Tools:          []interaction.Tool{{Type: "function", Function: interaction.ToolFunction{Name: "search", Parameters: json.RawMessage(`{"type":"object"}`)}}},
		ToolChoice:     json.RawMessage(`"auto"`),
		ResponseFormat: json.RawMessage(`{"type":"json_object"}`),
	}}
	encoded, err := EncodeChat(request)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	tools, _ := wire["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools missing: %s", encoded)
	}
	if wire["tool_choice"] != "auto" {
		t.Fatalf("tool_choice=%v", wire["tool_choice"])
	}
	if rf, _ := wire["response_format"].(map[string]any); rf == nil || rf["type"] != "json_object" {
		t.Fatalf("response_format=%v", wire["response_format"])
	}
	messages := wire["messages"].([]any)
	assistant := messages[1].(map[string]any)
	calls := assistant["tool_calls"].([]any)
	call := calls[0].(map[string]any)
	if call["id"] != "call_9" || call["function"].(map[string]any)["name"] != "search" {
		t.Fatalf("assistant tool_calls=%v", calls)
	}
	toolMsg := messages[2].(map[string]any)
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "call_9" {
		t.Fatalf("tool message=%v", toolMsg)
	}
}

func TestDecodeChatResponseWithToolCalls(t *testing.T) {
	body := []byte(`{"id":"c1","model":"gpt-4o","created":1,"choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":5}}`)
	response, err := DecodeChatResponse(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if response.StopReason != "tool_calls" {
		t.Fatalf("stop=%s", response.StopReason)
	}
	calls := response.Choices[0].Message.ToolCalls
	if len(calls) != 1 || calls[0].ID != "call_1" || calls[0].Function.Name != "get_weather" {
		t.Fatalf("calls=%+v", calls)
	}
}

func TestEncodeChatResponseWithToolCalls(t *testing.T) {
	response := &interaction.UnifiedResponse{ID: "c1", Model: "gpt-4o", StopReason: "tool_calls", Choices: []interaction.Choice{{Index: 0, Message: interaction.Message{Role: "assistant", ToolCalls: []interaction.ToolCall{{ID: "call_1", Type: "function", Function: interaction.ToolCallFunction{Name: "get_weather", Arguments: `{"city":"Paris"}`}}}}}}}
	encoded, err := EncodeChatResponse(response, "default-chat")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"finish_reason":"tool_calls"`) || !strings.Contains(string(encoded), `"tool_calls"`) {
		t.Fatalf("encoded=%s", encoded)
	}
}

func TestStreamToolCallDeltas(t *testing.T) {
	start, err := NormalizeStream([]byte(`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather"}}]}}]}`))
	if err != nil || len(start.ToolCallDeltas) != 1 {
		t.Fatalf("start=%+v err=%v", start, err)
	}
	if start.ToolCallDeltas[0].ID != "call_1" || start.ToolCallDeltas[0].Name != "get_weather" || start.ToolCallDeltas[0].Index != 0 {
		t.Fatalf("start delta=%+v", start.ToolCallDeltas[0])
	}
	frag, err := NormalizeStream([]byte(`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"ci"}}]}}]}`))
	if err != nil || len(frag.ToolCallDeltas) != 1 || frag.ToolCallDeltas[0].Arguments != `{"ci` {
		t.Fatalf("frag=%+v err=%v", frag, err)
	}
	final, err := NormalizeStream([]byte(`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`))
	if err != nil || !final.Final || final.StopReason != "tool_calls" {
		t.Fatalf("final=%+v err=%v", final, err)
	}

	chunk, err := EncodeStreamChunk(start, "default-chat")
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(chunk, &wire); err != nil {
		t.Fatal(err)
	}
	choice := wire["choices"].([]any)[0].(map[string]any)
	delta := choice["delta"].(map[string]any)
	calls := delta["tool_calls"].([]any)
	call := calls[0].(map[string]any)
	if call["id"] != "call_1" || call["index"] != float64(0) || call["function"].(map[string]any)["name"] != "get_weather" {
		t.Fatalf("chunk=%s", chunk)
	}
	fragChunk, err := EncodeStreamChunk(frag, "default-chat")
	if err != nil || !strings.Contains(string(fragChunk), `\"ci`) {
		t.Fatalf("fragChunk=%s err=%v", fragChunk, err)
	}
}

func TestPlainChatWithoutToolsStillCaches(t *testing.T) {
	request := &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "m", Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "hi"}}}}
	if request.ToolCalls() {
		t.Fatalf("plain chat must remain cacheable")
	}
}

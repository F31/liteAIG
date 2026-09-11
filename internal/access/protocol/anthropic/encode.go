package anthropic

import (
	"encoding/json"
	"fmt"

	"github.com/F31/liteAIG/internal/kernel/interaction"
)

// EncodeResponse renders a canonical chat response in the Anthropic Messages
// wire shape. model overrides the reported model name. Assistant tool calls
// become tool_use content blocks.
func EncodeResponse(response *interaction.UnifiedResponse, model string) ([]byte, error) {
	if response == nil {
		return nil, fmt.Errorf("response is required")
	}
	stop := response.StopReason
	if stop == "" {
		stop = "end_turn"
	}
	var blocks []map[string]any
	text := ""
	var calls []interaction.ToolCall
	if len(response.Choices) > 0 {
		text = response.Choices[0].Message.Content
		calls = response.Choices[0].Message.ToolCalls
	}
	if text != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": text})
	}
	for _, call := range calls {
		input := map[string]any{}
		if args := []byte(call.Function.Arguments); len(args) > 0 {
			if err := json.Unmarshal(args, &input); err != nil {
				input = map[string]any{"raw": call.Function.Arguments}
			}
		}
		blocks = append(blocks, map[string]any{"type": "tool_use", "id": call.ID, "name": call.Function.Name, "input": input})
	}
	if len(blocks) == 0 {
		blocks = []map[string]any{{"type": "text", "text": ""}}
	}
	wire := map[string]any{
		"id":            valueOr(response.ID, "msg_liteaig"),
		"type":          "message",
		"role":          "assistant",
		"model":         model,
		"content":       blocks,
		"stop_reason":   stop,
		"stop_sequence": nil,
		"usage": map[string]int64{
			"input_tokens":  response.Usage.InputTokens,
			"output_tokens": response.Usage.OutputTokens,
		},
	}
	return json.Marshal(wire)
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// SSEEvent is one named Server-Sent-Events frame for the Messages stream.
type SSEEvent struct {
	Name string
	Data []byte
}

// StreamEncoder turns normalized stream events into the Anthropic Messages
// SSE event sequence. It is stateful: it assigns Anthropic content-block
// indices as text and tool_use blocks are first seen, and emits the terminal
// frames on the final event.
type StreamEncoder struct {
	model   string
	id      string
	started bool
	done    bool

	nextBlock int
	textIndex int // client block index of the open text block, -1 if closed
	textOpen  bool
	toolIndex map[int]int // provider block index -> client block index
	toolOpen  []int       // client block indices still open, in open order
}

func NewStreamEncoder(model string) *StreamEncoder {
	return &StreamEncoder{model: model, id: "msg_liteaig", textIndex: -1, toolIndex: map[int]int{}}
}

func (e *StreamEncoder) begin(frames *[]SSEEvent) {
	if e.started {
		return
	}
	e.started = true
	start := map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": e.id, "type": "message", "role": "assistant", "model": e.model,
			"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
			"usage": map[string]int64{"input_tokens": 0, "output_tokens": 0},
		},
	}
	startData, _ := json.Marshal(start)
	*frames = append(*frames, SSEEvent{Name: "message_start", Data: startData})
}

func (e *StreamEncoder) closeTools(frames *[]SSEEvent) {
	for _, idx := range e.toolOpen {
		blockStop, _ := json.Marshal(map[string]any{"type": "content_block_stop", "index": idx})
		*frames = append(*frames, SSEEvent{Name: "content_block_stop", Data: blockStop})
	}
	e.toolOpen = nil
}

func (e *StreamEncoder) openText(frames *[]SSEEvent) {
	if e.textOpen {
		return
	}
	// Content blocks are sequential: close any open tool block before a new
	// text block starts.
	e.closeTools(frames)
	blockStart, _ := json.Marshal(map[string]any{"type": "content_block_start", "index": e.nextBlock, "content_block": map[string]string{"type": "text", "text": ""}})
	*frames = append(*frames, SSEEvent{Name: "content_block_start", Data: blockStart})
	e.textIndex = e.nextBlock
	e.textOpen = true
	e.nextBlock++
}

func (e *StreamEncoder) closeText(frames *[]SSEEvent) {
	if !e.textOpen {
		return
	}
	blockStop, _ := json.Marshal(map[string]any{"type": "content_block_stop", "index": e.textIndex})
	*frames = append(*frames, SSEEvent{Name: "content_block_stop", Data: blockStop})
	e.textOpen = false
}

// Write encodes one normalized event into zero or more SSE frames.
func (e *StreamEncoder) Write(event interaction.StreamEvent) ([]SSEEvent, error) {
	var frames []SSEEvent
	e.begin(&frames)

	for _, call := range event.ToolCallDeltas {
		if call.ID != "" {
			// A tool_use block starts: close the text block, open the tool block.
			e.closeText(&frames)
			if _, exists := e.toolIndex[call.Index]; !exists {
				blockStart, _ := json.Marshal(map[string]any{
					"type": "content_block_start", "index": e.nextBlock,
					"content_block": map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "input": map[string]any{}},
				})
				frames = append(frames, SSEEvent{Name: "content_block_start", Data: blockStart})
				e.toolIndex[call.Index] = e.nextBlock
				e.toolOpen = append(e.toolOpen, e.nextBlock)
				e.nextBlock++
			}
		}
		if call.Arguments != "" {
			if idx, ok := e.toolIndex[call.Index]; ok {
				delta, _ := json.Marshal(map[string]any{"type": "content_block_delta", "index": idx, "delta": map[string]any{"type": "input_json_delta", "partial_json": call.Arguments}})
				frames = append(frames, SSEEvent{Name: "content_block_delta", Data: delta})
			}
		}
	}
	if event.Delta != "" {
		e.openText(&frames)
		delta, _ := json.Marshal(map[string]any{"type": "content_block_delta", "index": e.textIndex, "delta": map[string]string{"type": "text_delta", "text": event.Delta}})
		frames = append(frames, SSEEvent{Name: "content_block_delta", Data: delta})
	}
	if event.Final || event.StopReason != "" {
		stop := event.StopReason
		if stop == "" {
			stop = "end_turn"
		}
		e.closeText(&frames)
		e.closeTools(&frames)
		messageDelta := map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stop, "stop_sequence": nil}}
		if event.Usage != nil {
			messageDelta["usage"] = map[string]int64{"output_tokens": event.Usage.OutputTokens}
		}
		deltaData, _ := json.Marshal(messageDelta)
		frames = append(frames, SSEEvent{Name: "message_delta", Data: deltaData})
		stopData, _ := json.Marshal(map[string]any{"type": "message_stop"})
		frames = append(frames, SSEEvent{Name: "message_stop", Data: stopData})
		e.done = true
	}
	return frames, nil
}

package a2a

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// LiteAIG A2A streaming profile.
//
// A2A 1.0 streams agent replies as Server-Sent Events. We control both sides of
// the wire (our outbound consumer and our inbound server), so we define one
// simple canonical profile instead of mirroring every A2A event spelling. The
// profile is documented in docs/A2A_STREAMING.md; this package owns the framing
// so the inbound server and the outbound connector share byte-identical wire
// events.
//
// Every SSE data line carries exactly one JSON object with a "type" discriminator:
//
//   - {"type":"message","message":{"parts":[{"kind":"text","text":"<delta>"}]}}
//     accumulates one text delta of the agent reply;
//   - {"type":"completed"} terminates a successful stream.
//
// No [DONE] marker is required (a compliant peer MAY still send one; consumers
// treat it as terminal). Comment/keep-alive and event lines are handled by the
// SSE readers, which deliver only data lines to ParseStreamFrame. A peer that
// fails mid-stream emits a JSON-RPC error object, which ParseStreamFrame
// surfaces as an error.

// textFrame is the wire shape of one message delta event. It deliberately uses
// the same Part JSON keys as TaskMessage so peers see A2A-shaped parts.
type textFrame struct {
	Type    string `json:"type"`
	Message struct {
		Parts []Part `json:"parts"`
	} `json:"message"`
}

// EncodeStreamChunk renders one message delta as a canonical SSE data frame.
func EncodeStreamChunk(delta string) ([]byte, error) {
	frame := textFrame{Type: "message"}
	frame.Message.Parts = []Part{{Kind: "text", Text: delta}}
	return json.Marshal(frame)
}

// EncodeStreamCompleted renders the terminal SSE data frame of a successful
// stream.
func EncodeStreamCompleted() ([]byte, error) {
	return json.Marshal(map[string]string{"type": "completed"})
}

// streamDataFrame is the parse view of a data frame: a canonical message or
// completed event, or a JSON-RPC error object.
type streamDataFrame struct {
	Type    string `json:"type"`
	Message *struct {
		Parts []Part `json:"parts"`
	} `json:"message"`
	Error *struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// ParseStreamFrame interprets one SSE data line of the LiteAIG A2A streaming
// profile. A message frame yields its concatenated text (newline-separated per
// A2A part semantics), a completed frame reports completion, and a JSON-RPC
// error object yields an error. Unknown or empty frames are ignored (delta "",
// completed false, nil error) so keep-alives and unrelated events never break a
// consumer.
func ParseStreamFrame(data []byte) (delta string, completed bool, err error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return "", false, nil
	}
	var frame streamDataFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		return "", false, err
	}
	if frame.Error != nil {
		message := frame.Error.Message
		if message == "" {
			message = "A2A stream error"
		}
		return "", false, fmt.Errorf("a2a stream error: %s", message)
	}
	switch frame.Type {
	case "completed":
		return "", true, nil
	case "message":
		if frame.Message == nil {
			return "", false, nil
		}
		var text strings.Builder
		for _, part := range frame.Message.Parts {
			if part.Kind != "text" {
				continue
			}
			if text.Len() > 0 {
				text.WriteString("\n")
			}
			text.WriteString(part.Text)
		}
		return text.String(), false, nil
	default:
		return "", false, nil
	}
}

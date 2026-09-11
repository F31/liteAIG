// Package a2a normalizes A2A 1.0 Agent Card discovery and Task/Message traffic
// into the unified interaction model. Discovery only yields a candidate; it
// never creates trust. A card alone is never trust: signature verification
// (VerifyCardSignature) proves origin/integrity of a candidate, and activation
// is a separate lifecycle decision taken by the federation domain.
package a2a

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/F31/liteAIG/internal/kernel/interaction"
)

const ProtocolVersion = "1.0.0"

// AgentCard is the A2A 1.0 agent discovery resource.
type AgentCard struct {
	Name         string   `json:"name"`
	URL          string   `json:"url"`
	Description  string   `json:"description"`
	Version      string   `json:"version"`
	Skills       []string `json:"skills"`
	Capabilities []string `json:"capabilities"`
	Publisher    string   `json:"publisher"`
	// Signature is the optional JWS covering the card's canonical form. Legacy
	// string signatures are not part of the wire model; a non-nil Signature
	// MUST carry the structured CardSignature shape.
	Signature *CardSignature `json:"signature,omitempty"`
}

// CardSignature is the JWS attached to a signed Agent Card. The header is the
// minimal {"alg":<alg>} protected header reconstructed during verification;
// KeyID is carried on the card as the operator-facing key selector only and is
// not part of the signing input.
type CardSignature struct {
	Alg   string `json:"alg"`
	KeyID string `json:"kid,omitempty"`
	Value string `json:"value"`
}

// TaskMessage is an A2A task message.
type TaskMessage struct {
	MessageID string            `json:"messageId"`
	Role      string            `json:"role"`
	Parts     []Part            `json:"parts"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// Part is a text or artifact part.
type Part struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// NormalizeAgentCard turns an A2A Agent Card into a candidate interaction.
// The returned request is a candidate target; no trust is implied.
func NormalizeAgentCard(raw []byte) (*interaction.UnifiedRequest, *AgentCard, error) {
	var card AgentCard
	if err := json.Unmarshal(raw, &card); err != nil {
		return nil, nil, err
	}
	if card.Name == "" || card.URL == "" {
		return nil, nil, errors.New("invalid A2A agent card")
	}
	request := &interaction.UnifiedRequest{
		Kind:  interaction.RequestTool,
		Model: card.Name,
		Tool:  &interaction.ToolPayload{Name: card.Name, Arguments: raw},
	}
	return request, &card, nil
}

// NormalizeTaskMessage turns an A2A task message into a chat interaction.
// Text parts are concatenated in order (newline-separated) so no part is lost;
// the A2A messageId is preserved on the request session for correlation, and
// A2A roles map onto the canonical chat roles.
func NormalizeTaskMessage(raw []byte) (*interaction.UnifiedRequest, error) {
	var message TaskMessage
	if err := json.Unmarshal(raw, &message); err != nil {
		return nil, err
	}
	if message.MessageID == "" {
		return nil, errors.New("A2A message id is required")
	}
	role := normalizeRole(message.Role)
	var text strings.Builder
	for _, part := range message.Parts {
		if part.Kind != "" && part.Kind != "text" {
			continue
		}
		if text.Len() > 0 {
			text.WriteString("\n")
		}
		text.WriteString(part.Text)
	}
	return &interaction.UnifiedRequest{
		Kind:      interaction.RequestChat,
		Chat:      &interaction.ChatPayload{Messages: []interaction.Message{{Role: role, Content: text.String()}}},
		Metadata:  message.Metadata,
		SessionID: message.MessageID,
	}, nil
}

// normalizeRole maps an A2A role string onto the canonical chat role. Both the
// 0.3-style lowercase values ("user"/"agent") and the 1.0 protobuf-JSON enum
// names ("ROLE_USER"/"ROLE_AGENT") are accepted.
func normalizeRole(role string) string {
	switch role {
	case "ROLE_AGENT", "agent":
		return "assistant"
	case "ROLE_USER", "user":
		return "user"
	default:
		return "user"
	}
}

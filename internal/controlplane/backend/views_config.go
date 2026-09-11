package backend

import (
	"time"
)

// RebaseResult reports a draft rebase or conflict.
type RebaseResult struct {
	Rebased    bool   `json:"rebased"`
	Conflict   bool   `json:"conflict"`
	Revision   int64  `json:"revision"`
	MessageKey string `json:"messageKey"`
}

// DraftSummary is the list-surface projection of a draft; the editor loads
// the full document (config included) via GET /drafts/{id}.
type DraftSummary struct {
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	BaseVersion int64     `json:"baseVersion"`
	Revision    int64     `json:"revision"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

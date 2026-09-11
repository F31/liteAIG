package federation

import (
	"context"
	"time"
)

// A2APushDelivery is the durable callback payload produced after a completed
// inbound A2A relay. Payload is already redacted protocol JSON; URL/token must
// never be logged.
type A2APushDelivery struct {
	ID          string
	TaskID      string
	CallbackURL string
	BearerToken string
	Payload     []byte
	Attempts    int
	MaxAttempts int
}

// A2APushOutbox is the storage boundary used by the gateway push worker. The
// enqueue call derives tenant scope from the durable A2A task row.
type A2APushOutbox interface {
	EnqueueForTask(ctx context.Context, delivery A2APushDelivery) error
	Due(ctx context.Context, limit int, now time.Time) ([]A2APushDelivery, error)
	MarkDelivered(ctx context.Context, id string) error
	MarkAttempt(ctx context.Context, id string, nextAttemptAt time.Time, exhausted bool, reason string) error
}

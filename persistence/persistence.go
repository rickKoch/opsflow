package persistence

import "context"

// PID is a portable identifier for an actor.
type PID string

// Persistence is an abstraction for actor state and mailbox storage.
type Persistence interface {
	SaveSnapshot(ctx context.Context, id PID, data []byte) error
	LoadSnapshot(ctx context.Context, id PID) ([]byte, error)
	SaveMailbox(ctx context.Context, id PID, messages [][]byte) error
	LoadMailbox(ctx context.Context, id PID) ([][]byte, error)
	// Optional: implementers may choose to list stored keys. If not
	// implemented, callers should gracefully fall back.
	ListKeys(ctx context.Context, prefix PID) ([]PID, error)
}

package codex

import "context"

// CodexClient defines the interface for Codex operations
type CodexClient interface {
	Start(ctx context.Context) error
	Stop() error
	Events() <-chan Event
	IsRunning() bool
	ThreadStart(ctx context.Context, params *ThreadStartParams) (string, error)
	ThreadResume(ctx context.Context, threadID string) (*Thread, error)
	TurnStart(ctx context.Context, threadID, prompt string, images []string) (string, error)
	TurnInterrupt(ctx context.Context, threadID string) error
	RespondToApproval(requestID int64, decision string) error
}

// Ensure Client implements CodexClient
var _ CodexClient = (*Client)(nil)

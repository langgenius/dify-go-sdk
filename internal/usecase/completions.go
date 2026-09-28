package usecase

import (
	"context"
	"net/http"

	"github.com/langgenius/dify-go-sdk/internal/codec"
	"github.com/langgenius/dify-go-sdk/internal/entity"
	"github.com/langgenius/dify-go-sdk/internal/kernel"
	"github.com/langgenius/dify-go-sdk/internal/port"
)

// CompletionParams are the optional parts of a completion.
type CompletionParams struct {
	User  string
	Files []map[string]any
	// KeepErrors yields an "error" event as an ordinary event instead of
	// ending the stream with an *APIError. Streams only.
	KeepErrors bool
}

// Completions are one prompt in, one answer out, no thread. Only a
// completion-mode app serves these.
type Completions struct{ api port.Port }

func (c *Completions) body(inputs map[string]any, mode string, p *CompletionParams) (map[string]any, error) {
	if p == nil {
		p = &CompletionParams{}
	}
	user, err := c.api.Who(p.User)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"inputs": kernel.OrEmpty(inputs), "response_mode": mode, "user": user}
	if p.Files != nil {
		body["files"] = p.Files
	}
	return body, nil
}

// Create sends a prompt and waits for the answer. The prompt goes in inputs,
// under whichever variable the app declares — usually "query".
func (c *Completions) Create(ctx context.Context, inputs map[string]any, p *CompletionParams) (*entity.Message, error) {
	body, err := c.body(inputs, "blocking", p)
	if err != nil {
		return nil, err
	}
	o, err := c.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/completion-messages", Body: body})
	if err != nil {
		return nil, err
	}
	return codec.MessageFromBlocking(o), nil
}

// Stream sends a prompt and returns the answer as it arrives.
func (c *Completions) Stream(ctx context.Context, inputs map[string]any, p *CompletionParams) (*codec.MessageStream, error) {
	body, err := c.body(inputs, "streaming", p)
	if err != nil {
		return nil, err
	}
	events, err := c.api.Stream(ctx, &port.Request{Method: http.MethodPost, Path: "/completion-messages", Body: body})
	if err != nil {
		return nil, err
	}
	return codec.NewMessageStream(events, p == nil || !p.KeepErrors), nil
}

// Stop stops a completion that is still being written, by its TaskID.
func (c *Completions) Stop(ctx context.Context, taskID, user string) error {
	return stopTask(ctx, c.api, "/completion-messages/", taskID, user, "completion")
}

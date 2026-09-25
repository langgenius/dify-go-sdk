package dify

import (
	"context"
	"net/http"
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
type Completions struct{ t *transport }

func (c *Completions) body(inputs map[string]any, mode string, p *CompletionParams) (map[string]any, error) {
	if p == nil {
		p = &CompletionParams{}
	}
	user, err := c.t.who(p.User)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"inputs": orEmpty(inputs), "response_mode": mode, "user": user}
	if p.Files != nil {
		body["files"] = p.Files
	}
	return body, nil
}

// Create sends a prompt and waits for the answer. The prompt goes in inputs,
// under whichever variable the app declares — usually "query".
func (c *Completions) Create(ctx context.Context, inputs map[string]any, p *CompletionParams) (*Message, error) {
	body, err := c.body(inputs, "blocking", p)
	if err != nil {
		return nil, err
	}
	o, err := c.t.call(ctx, &request{method: http.MethodPost, path: "/completion-messages", body: body})
	if err != nil {
		return nil, err
	}
	return messageFromBlocking(o), nil
}

// Stream sends a prompt and returns the answer as it arrives.
func (c *Completions) Stream(ctx context.Context, inputs map[string]any, p *CompletionParams) (*MessageStream, error) {
	body, err := c.body(inputs, "streaming", p)
	if err != nil {
		return nil, err
	}
	resp, _, err := c.t.send(ctx, &request{method: http.MethodPost, path: "/completion-messages", body: body, stream: true})
	if err != nil {
		return nil, err
	}
	return &MessageStream{newEventStream(resp.Body, p == nil || !p.KeepErrors)}, nil
}

// Stop stops a completion that is still being written, by its TaskID.
func (c *Completions) Stop(ctx context.Context, taskID, user string) error {
	return stopTask(ctx, c.t, "/completion-messages/", taskID, user, "completion")
}

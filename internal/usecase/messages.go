package usecase

import (
	"context"
	"net/http"

	"github.com/langgenius/dify-go-sdk/internal/codec"
	"github.com/langgenius/dify-go-sdk/internal/entity"
	"github.com/langgenius/dify-go-sdk/internal/kernel"
	"github.com/langgenius/dify-go-sdk/internal/port"
)

// MessageParams are the optional parts of sending a chat message.
type MessageParams struct {
	// Inputs are the start-node variables, if the app declares any. A
	// chatflow needs the query and its start inputs, not one or the other.
	Inputs map[string]any
	// User is the end-user identifier. Empty uses WithUser.
	User string
	// ConversationID continues a thread. Empty starts a new one, whose id
	// comes back on the message.
	ConversationID string
	// Files to attach, in Dify's file mapping shape — UploadedFile.Reference
	// builds one.
	Files []map[string]any
	// WorkflowID pins a chatflow to a published version other than the
	// current one.
	WorkflowID string
	// NoAutoName stops Dify naming a new conversation from its first turn.
	NoAutoName bool
	// KeepErrors yields an "error" event as an ordinary event instead of
	// ending the stream with an *APIError. Streams only.
	KeepErrors bool
}

// Messages are the messages in this app's conversations.
//
// Create waits for the whole answer. Stream hands it back as it is written.
// Both return the thread's ConversationID, which is what continues it.
type Messages struct{ api port.Port }

func (m *Messages) body(query, mode string, p *MessageParams) (map[string]any, error) {
	if p == nil {
		p = &MessageParams{}
	}
	user, err := m.api.Who(p.User)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"query":         query,
		"inputs":        kernel.OrEmpty(p.Inputs),
		"response_mode": mode,
		"user":          user,
	}
	if p.ConversationID != "" {
		body["conversation_id"] = p.ConversationID
	}
	if p.Files != nil {
		body["files"] = p.Files
	}
	if p.WorkflowID != "" {
		body["workflow_id"] = p.WorkflowID
	}
	if p.NoAutoName {
		body["auto_generate_name"] = false
	}
	return body, nil
}

// Create sends a message and waits for the answer.
func (m *Messages) Create(ctx context.Context, query string, p *MessageParams) (*entity.Message, error) {
	body, err := m.body(query, "blocking", p)
	if err != nil {
		return nil, err
	}
	o, err := m.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/chat-messages", Body: body})
	if err != nil {
		return nil, err
	}
	return codec.MessageFromBlocking(o), nil
}

// Stream sends a message and returns the answer as it arrives. Close the
// stream when done; closing stops watching, not the answer being written.
func (m *Messages) Stream(ctx context.Context, query string, p *MessageParams) (*codec.MessageStream, error) {
	body, err := m.body(query, "streaming", p)
	if err != nil {
		return nil, err
	}
	events, err := m.api.Stream(ctx, &port.Request{Method: http.MethodPost, Path: "/chat-messages", Body: body})
	if err != nil {
		return nil, err
	}
	return codec.NewMessageStream(events, p == nil || !p.KeepErrors), nil
}

// HistoryParams narrow a conversation's history.
type HistoryParams struct {
	User string
	// FirstID starts the listing before this message.
	FirstID string
	Limit   int
}

// List is one conversation's turns, Newest page first.
//
// Each page arrives Oldest-first and the page after it is older, so walking
// continues from the first id on each page. One thing this cannot paper
// over: Dify's cursor compares created_at, which it stores to the second, and
// excludes ties, so messages written inside the same second can be skipped by
// any client paging this listing. Read such a conversation in one page, with
// Limit above its length, if that matters.
func (m *Messages) List(ctx context.Context, conversationID string, p *HistoryParams) (*codec.Page[entity.HistoryMessage], error) {
	if p == nil {
		p = &HistoryParams{}
	}
	user, err := m.api.Who(p.User)
	if err != nil {
		return nil, err
	}
	fetch := func(ctx context.Context, cursor string) (kernel.Object, error) {
		q := port.Params{}.Set("conversation_id", conversationID).Set("user", user).
			Set("first_id", kernel.FirstNonZero(cursor, p.FirstID)).SetInt("limit", p.Limit)
		return m.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/messages", Query: q.Values()})
	}
	return codec.FetchByCursor(ctx, codec.HistoryFrom, fetch, codec.Oldest)
}

// Stop stops an answer that is still being written, by the TaskID a streamed
// message carries.
func (m *Messages) Stop(ctx context.Context, taskID, user string) error {
	return stopTask(ctx, m.api, "/chat-messages/", taskID, user, "message")
}

func stopTask(ctx context.Context, api port.Port, prefix, taskID, user, what string) error {
	if taskID == "" {
		return kernel.ArgError("this %s carries no task_id, so there is nothing to stop; only a streamed %s reports one", what, what)
	}
	who, err := api.Who(user)
	if err != nil {
		return err
	}
	_, err = api.Call(ctx, &port.Request{Method: http.MethodPost, Path: prefix + port.PathEscape(taskID) + "/stop", Body: map[string]any{"user": who}})
	return err
}

// Rating is feedback on a message.
type Rating string

const (
	Like    Rating = "like"
	Dislike Rating = "dislike"
	// NoRating takes a rating back.
	NoRating Rating = ""
)

// FeedbackParams are the optional parts of rating a message.
type FeedbackParams struct {
	User    string
	Content string
}

// Feedback rates a message, or takes a rating back with NoRating.
func (m *Messages) Feedback(ctx context.Context, messageID string, rating Rating, p *FeedbackParams) error {
	if p == nil {
		p = &FeedbackParams{}
	}
	user, err := m.api.Who(p.User)
	if err != nil {
		return err
	}
	// A null rating is a value, not an omission: it is how a rating is
	// revoked, so it is sent rather than left out.
	var r any
	if rating != NoRating {
		r = string(rating)
	}
	body := map[string]any{"rating": r, "user": user}
	if p.Content != "" {
		body["content"] = p.Content
	}
	_, err = m.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/messages/" + port.PathEscape(messageID) + "/feedbacks", Body: body})
	return err
}

// Suggested is what Dify suggests the user might ask next.
func (m *Messages) Suggested(ctx context.Context, messageID, user string) ([]string, error) {
	who, err := m.api.Who(user)
	if err != nil {
		return nil, err
	}
	o, err := m.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/messages/" + port.PathEscape(messageID) + "/suggested", Query: port.Params{}.Set("user", who).Values()})
	if err != nil {
		return nil, err
	}
	return codec.SuggestionsFrom(o), nil
}

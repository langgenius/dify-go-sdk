package usecase

import (
	"context"
	"net/http"

	"github.com/langgenius/dify-go-sdk/internal/codec"
	"github.com/langgenius/dify-go-sdk/internal/entity"
	"github.com/langgenius/dify-go-sdk/internal/kernel"
	"github.com/langgenius/dify-go-sdk/internal/port"
)

// Conversations are a user's threads with this app.
type Conversations struct{ api port.Port }

// ConversationListParams narrow a user's conversations.
type ConversationListParams struct {
	User string
	// LastID starts the listing after this conversation.
	LastID string
	Limit  int
	// SortBy is created_at, -created_at, updated_at or -updated_at. Empty
	// is Dify's default, -updated_at.
	SortBy string
}

// List is a user's conversations. It pages by cursor, Newest first, so the
// next page continues from the last id on this one — Dify's last_id.
func (c *Conversations) List(ctx context.Context, p *ConversationListParams) (*codec.Page[entity.Conversation], error) {
	if p == nil {
		p = &ConversationListParams{}
	}
	user, err := c.api.Who(p.User)
	if err != nil {
		return nil, err
	}
	fetch := func(ctx context.Context, cursor string) (kernel.Object, error) {
		q := port.Params{}.Set("user", user).Set("last_id", kernel.FirstNonZero(cursor, p.LastID)).
			SetInt("limit", p.Limit).Set("sort_by", p.SortBy)
		return c.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/conversations", Query: q.Values()})
	}
	return codec.FetchByCursor(ctx, codec.ConversationFrom, fetch, codec.Newest)
}

// RenameParams choose how a thread is renamed.
type RenameParams struct {
	User string
	// AutoGenerate has Dify name the thread from its contents; Name is then
	// ignored.
	AutoGenerate bool
}

// Rename renames a thread, or has Dify name it from its contents.
func (c *Conversations) Rename(ctx context.Context, conversationID, name string, p *RenameParams) (*entity.Conversation, error) {
	if p == nil {
		p = &RenameParams{}
	}
	user, err := c.api.Who(p.User)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"user": user, "auto_generate": p.AutoGenerate}
	if name != "" {
		body["name"] = name
	}
	o, err := c.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/conversations/" + port.PathEscape(conversationID) + "/name", Body: body})
	if err != nil {
		return nil, err
	}
	conv := codec.ConversationFrom(o)
	return &conv, nil
}

// Delete deletes a thread and its messages.
func (c *Conversations) Delete(ctx context.Context, conversationID, user string) error {
	who, err := c.api.Who(user)
	if err != nil {
		return err
	}
	_, err = c.api.Call(ctx, &port.Request{Method: http.MethodDelete, Path: "/conversations/" + port.PathEscape(conversationID), Body: map[string]any{"user": who}})
	return err
}

// VariableParams narrow a thread's variables.
type VariableParams struct {
	User string
	// LastID starts the listing after this variable. Conversation variables
	// page by cursor, not by page number.
	LastID string
	Limit  int
	// Name filters to one variable by name.
	Name string
}

// Variables is what a thread has accumulated. Conversation variables persist
// across turns, which is how a chatflow remembers what it was told earlier.
func (c *Conversations) Variables(ctx context.Context, conversationID string, p *VariableParams) (*codec.Page[map[string]any], error) {
	if p == nil {
		p = &VariableParams{}
	}
	user, err := c.api.Who(p.User)
	if err != nil {
		return nil, err
	}
	fetch := func(ctx context.Context, cursor string) (kernel.Object, error) {
		q := port.Params{}.Set("user", user).Set("last_id", kernel.FirstNonZero(cursor, p.LastID)).
			SetInt("limit", p.Limit).Set("variable_name", p.Name)
		return c.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/conversations/" + port.PathEscape(conversationID) + "/variables", Query: q.Values()})
	}
	return codec.FetchByCursor(ctx, kernel.Object.Raw, fetch, codec.Newest)
}

// SetVariable changes one variable a thread is carrying — how a caller seeds
// or corrects a chatflow's memory from outside.
func (c *Conversations) SetVariable(ctx context.Context, conversationID, variableID string, value any, user string) (map[string]any, error) {
	who, err := c.api.Who(user)
	if err != nil {
		return nil, err
	}
	o, err := c.api.Call(ctx, &port.Request{
		Method: http.MethodPut,
		Path:   "/conversations/" + port.PathEscape(conversationID) + "/variables/" + port.PathEscape(variableID),
		Body:   map[string]any{"value": value, "user": who},
	})
	return o.Raw(), err
}

package dify

import (
	"context"
	"net/http"
)

// Conversation is one thread, belonging to one user.
type Conversation struct {
	ID           string
	Name         string
	Status       string
	Introduction string
	Inputs       map[string]any
	CreatedAt    *int64
	UpdatedAt    *int64
	Raw          map[string]any
}

func conversationFrom(o object) Conversation {
	return Conversation{
		ID:           o.str("id"),
		Name:         o.str("name"),
		Status:       o.str("status"),
		Introduction: o.str("introduction"),
		Inputs:       o.obj("inputs").raw(),
		CreatedAt:    o.intPtr("created_at"),
		UpdatedAt:    o.intPtr("updated_at"),
		Raw:          o.raw(),
	}
}

// Conversations are a user's threads with this app.
type Conversations struct{ api port }

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

// List is a user's conversations. It pages by cursor, newest first, so the
// next page continues from the last id on this one — Dify's last_id.
func (c *Conversations) List(ctx context.Context, p *ConversationListParams) (*Page[Conversation], error) {
	if p == nil {
		p = &ConversationListParams{}
	}
	user, err := c.api.who(p.User)
	if err != nil {
		return nil, err
	}
	fetch := func(ctx context.Context, cursor string) (object, error) {
		q := params{}.set("user", user).set("last_id", firstNonZero(cursor, p.LastID)).
			setInt("limit", p.Limit).set("sort_by", p.SortBy)
		return c.api.call(ctx, &request{method: http.MethodGet, path: "/conversations", query: q.values()})
	}
	return fetchByCursor(ctx, conversationFrom, fetch, newest)
}

// RenameParams choose how a thread is renamed.
type RenameParams struct {
	User string
	// AutoGenerate has Dify name the thread from its contents; Name is then
	// ignored.
	AutoGenerate bool
}

// Rename renames a thread, or has Dify name it from its contents.
func (c *Conversations) Rename(ctx context.Context, conversationID, name string, p *RenameParams) (*Conversation, error) {
	if p == nil {
		p = &RenameParams{}
	}
	user, err := c.api.who(p.User)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"user": user, "auto_generate": p.AutoGenerate}
	if name != "" {
		body["name"] = name
	}
	o, err := c.api.call(ctx, &request{method: http.MethodPost, path: "/conversations/" + pathEscape(conversationID) + "/name", body: body})
	if err != nil {
		return nil, err
	}
	conv := conversationFrom(o)
	return &conv, nil
}

// Delete deletes a thread and its messages.
func (c *Conversations) Delete(ctx context.Context, conversationID, user string) error {
	who, err := c.api.who(user)
	if err != nil {
		return err
	}
	_, err = c.api.call(ctx, &request{method: http.MethodDelete, path: "/conversations/" + pathEscape(conversationID), body: map[string]any{"user": who}})
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
func (c *Conversations) Variables(ctx context.Context, conversationID string, p *VariableParams) (*Page[map[string]any], error) {
	if p == nil {
		p = &VariableParams{}
	}
	user, err := c.api.who(p.User)
	if err != nil {
		return nil, err
	}
	fetch := func(ctx context.Context, cursor string) (object, error) {
		q := params{}.set("user", user).set("last_id", firstNonZero(cursor, p.LastID)).
			setInt("limit", p.Limit).set("variable_name", p.Name)
		return c.api.call(ctx, &request{method: http.MethodGet, path: "/conversations/" + pathEscape(conversationID) + "/variables", query: q.values()})
	}
	return fetchByCursor(ctx, object.raw, fetch, newest)
}

// SetVariable changes one variable a thread is carrying — how a caller seeds
// or corrects a chatflow's memory from outside.
func (c *Conversations) SetVariable(ctx context.Context, conversationID, variableID string, value any, user string) (map[string]any, error) {
	who, err := c.api.who(user)
	if err != nil {
		return nil, err
	}
	o, err := c.api.call(ctx, &request{
		method: http.MethodPut,
		path:   "/conversations/" + pathEscape(conversationID) + "/variables/" + pathEscape(variableID),
		body:   map[string]any{"value": value, "user": who},
	})
	return o.raw(), err
}

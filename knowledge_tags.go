package dify

import (
	"context"
	"net/http"
)

// Tag is a label across the workspace's knowledge bases.
type Tag struct {
	ID   string
	Name string
	Type string
	// BindingCount is how many knowledge bases carry it. Dify sends this as a
	// *string* on the wire, so comparing the raw payload's value to an int
	// compares a str to an int; this is the parsed form.
	BindingCount int
	Raw          map[string]any
}

func (t *Tag) String() string { return t.Name }

func tagFrom(o object) *Tag {
	count := o.str("binding_count")
	n := 0
	if count != "" {
		digits := true
		for _, c := range count {
			if c < '0' || c > '9' {
				digits = false
				break
			}
		}
		if digits {
			for _, c := range count {
				n = n*10 + int(c-'0')
			}
		}
	}
	return &Tag{
		ID:           o.str("id"),
		Name:         o.str("name"),
		Type:         o.str("type"),
		BindingCount: n,
		Raw:          o.raw(),
	}
}

// Tags are tags across the workspace's knowledge bases.
//
// Workspace-level, not per-dataset: a tag exists once and is bound to as
// many knowledge bases as you like. Datasets.Tags reads the other
// direction — which tags one base carries.
type Tags struct{ t *transport }

// List lists every tag in the workspace.
//
// Dify answers this one with a bare array and no paging at all, so the page
// holds the lot — still a Page, so a caller need not know which listings
// page and which do not.
func (tg *Tags) List(ctx context.Context) (*Page[*Tag], error) {
	o, err := tg.t.call(ctx, &request{method: http.MethodGet, path: "/datasets/tags"})
	if err != nil {
		return nil, err
	}
	return unpaged(buildAll(o.objs("data"), tagFrom)), nil
}

// Create adds a tag.
func (tg *Tags) Create(ctx context.Context, name string) (*Tag, error) {
	o, err := tg.t.call(ctx, &request{method: http.MethodPost, path: "/datasets/tags", body: map[string]any{"name": name, "type": "knowledge"}})
	if err != nil {
		return nil, err
	}
	return tagFrom(o), nil
}

// Rename changes a tag's name. Its bindings are kept.
func (tg *Tags) Rename(ctx context.Context, tagID, name string) (*Tag, error) {
	o, err := tg.t.call(ctx, &request{method: http.MethodPatch, path: "/datasets/tags", body: map[string]any{"tag_id": tagID, "name": name}})
	if err != nil {
		return nil, err
	}
	return tagFrom(o), nil
}

// Delete removes a tag from the workspace, and from everything it was on.
func (tg *Tags) Delete(ctx context.Context, tagID string) error {
	_, err := tg.t.call(ctx, &request{method: http.MethodDelete, path: "/datasets/tags", body: map[string]any{"tag_id": tagID}})
	return err
}

// Bind puts these tags on a knowledge base.
func (tg *Tags) Bind(ctx context.Context, datasetID string, tagIDs []string) error {
	_, err := tg.t.call(ctx, &request{method: http.MethodPost, path: "/datasets/tags/binding", body: map[string]any{"tag_ids": tagIDs, "target_id": datasetID}})
	return err
}

// Unbind takes tags off a knowledge base. The tags themselves remain.
//
// Sends tag_ids. Dify still accepts a singular tag_id and marks it
// deprecated in the payload's own schema — a route being current does not
// make every field on it current.
func (tg *Tags) Unbind(ctx context.Context, datasetID string, tagIDs ...string) error {
	if len(tagIDs) == 0 {
		return argError("name at least one tag to unbind")
	}
	_, err := tg.t.call(ctx, &request{method: http.MethodPost, path: "/datasets/tags/unbinding", body: map[string]any{"tag_ids": tagIDs, "target_id": datasetID}})
	return err
}

package usecase

import (
	"context"
	"net/http"

	"github.com/langgenius/dify-go-sdk/internal/codec"
	"github.com/langgenius/dify-go-sdk/internal/entity"
	"github.com/langgenius/dify-go-sdk/internal/kernel"
	"github.com/langgenius/dify-go-sdk/internal/port"
)

// Tags are tags across the workspace's knowledge bases.
//
// Workspace-level, not per-dataset: a tag exists once and is bound to as
// many knowledge bases as you like. Datasets.Tags reads the other
// direction — which tags one base carries.
type Tags struct{ api port.Port }

// List lists every tag in the workspace.
//
// Dify answers this one with a bare array and no paging at all, so the page
// holds the lot — still a Page, so a caller need not know which listings
// page and which do not.
func (tg *Tags) List(ctx context.Context) (*codec.Page[*entity.Tag], error) {
	o, err := tg.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/datasets/tags"})
	if err != nil {
		return nil, err
	}
	return codec.Unpaged(codec.TagsFrom(o)), nil
}

// Create adds a tag.
func (tg *Tags) Create(ctx context.Context, name string) (*entity.Tag, error) {
	o, err := tg.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/datasets/tags", Body: map[string]any{"name": name, "type": "knowledge"}})
	if err != nil {
		return nil, err
	}
	return codec.TagFrom(o), nil
}

// Rename changes a tag's name. Its bindings are kept.
func (tg *Tags) Rename(ctx context.Context, tagID, name string) (*entity.Tag, error) {
	o, err := tg.api.Call(ctx, &port.Request{Method: http.MethodPatch, Path: "/datasets/tags", Body: map[string]any{"tag_id": tagID, "name": name}})
	if err != nil {
		return nil, err
	}
	return codec.TagFrom(o), nil
}

// Delete removes a tag from the workspace, and from everything it was on.
func (tg *Tags) Delete(ctx context.Context, tagID string) error {
	_, err := tg.api.Call(ctx, &port.Request{Method: http.MethodDelete, Path: "/datasets/tags", Body: map[string]any{"tag_id": tagID}})
	return err
}

// Bind puts these tags on a knowledge base.
func (tg *Tags) Bind(ctx context.Context, datasetID string, tagIDs []string) error {
	_, err := tg.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/datasets/tags/binding", Body: map[string]any{"tag_ids": tagIDs, "target_id": datasetID}})
	return err
}

// Unbind takes tags off a knowledge base. The tags themselves remain.
//
// Sends tag_ids. Dify still accepts a singular tag_id and marks it
// deprecated in the payload's own schema — a route being current does not
// make every field on it current.
func (tg *Tags) Unbind(ctx context.Context, datasetID string, tagIDs ...string) error {
	if len(tagIDs) == 0 {
		return kernel.ArgError("name at least one tag to unbind")
	}
	_, err := tg.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/datasets/tags/unbinding", Body: map[string]any{"tag_ids": tagIDs, "target_id": datasetID}})
	return err
}

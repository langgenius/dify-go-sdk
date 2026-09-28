package usecase

import (
	"context"
	"net/http"

	"github.com/langgenius/dify-go-sdk/internal/codec"
	"github.com/langgenius/dify-go-sdk/internal/entity"
	"github.com/langgenius/dify-go-sdk/internal/kernel"
	"github.com/langgenius/dify-go-sdk/internal/port"
)

// Segments are the chunks of one document, and the child chunks beneath
// them.
type Segments struct {
	api        port.Port
	datasetID  string
	documentID string
}

func (s *Segments) path(parts ...string) string {
	return datasetPath(s.datasetID, append([]string{"documents", port.PathEscape(s.documentID)}, parts...)...)
}

// SegmentListParams narrow a document's segment listing.
type SegmentListParams struct {
	Keyword string
	// Status filters by indexing status, e.g. "completed", "indexing" or
	// "error".
	Status string
	Page   int
	Limit  int
}

// List lists this document's segments.
func (s *Segments) List(ctx context.Context, p *SegmentListParams) (*codec.Page[*entity.Segment], error) {
	if p == nil {
		p = &SegmentListParams{}
	}
	fetch := func(ctx context.Context, number int) (kernel.Object, error) {
		q := port.Params{}.Set("keyword", p.Keyword).Set("status", p.Status).SetInt("page", number).SetInt("limit", p.Limit)
		return s.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: s.path("segments"), Query: q.Values()})
	}
	return codec.FetchByPage(ctx, codec.SegmentFrom, fetch, p.Page)
}

// Retrieve reads one segment back.
func (s *Segments) Retrieve(ctx context.Context, segmentID string) (*entity.Segment, error) {
	o, err := s.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: s.path("segments", port.PathEscape(segmentID))})
	if err != nil {
		return nil, err
	}
	return codec.SegmentFromEnvelope(o), nil
}

// Create adds segments by hand, rather than letting Dify chunk. Each map is
// {"content": ..., "answer": ..., "keywords": [...]}.
func (s *Segments) Create(ctx context.Context, segments []map[string]any) ([]*entity.Segment, error) {
	o, err := s.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: s.path("segments"), Body: map[string]any{"segments": segments}})
	if err != nil {
		return nil, err
	}
	return codec.SegmentsFrom(o), nil
}

// Update changes one segment. fields is merged onto Dify's own
// "segment": {...} body — {"content": ..., "answer": ..., "keywords": [...],
// "enabled": ...}.
func (s *Segments) Update(ctx context.Context, segmentID string, fields map[string]any) (*entity.Segment, error) {
	o, err := s.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: s.path("segments", port.PathEscape(segmentID)), Body: map[string]any{"segment": fields}})
	if err != nil {
		return nil, err
	}
	return codec.SegmentFromEnvelope(o), nil
}

// Delete removes one segment.
func (s *Segments) Delete(ctx context.Context, segmentID string) error {
	_, err := s.api.Call(ctx, &port.Request{Method: http.MethodDelete, Path: s.path("segments", port.PathEscape(segmentID))})
	return err
}

// ChildChunkListParams narrow a segment's child-chunk listing.
type ChildChunkListParams struct {
	Keyword string
	// Page defaults to 1, Limit to 20 — Dify's own defaults for this route,
	// since it has no has_more/limit envelope shortcut the way most listings
	// do.
	Page  int
	Limit int
}

// ChildChunks lists the child chunks of one segment, for parent-child
// indexing. Left as maps: the shape is open-ended and not worth a type on
// its own.
func (s *Segments) ChildChunks(ctx context.Context, segmentID string, p *ChildChunkListParams) ([]map[string]any, error) {
	if p == nil {
		p = &ChildChunkListParams{}
	}
	q := port.Params{}.SetInt("page", kernel.FirstNonZero(p.Page, 1)).SetInt("limit", kernel.FirstNonZero(p.Limit, 20)).Set("keyword", p.Keyword)
	o, err := s.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: s.path("segments", port.PathEscape(segmentID), "child_chunks"), Query: q.Values()})
	if err != nil {
		return nil, err
	}
	return codec.DataMaps(o), nil
}

// AddChildChunk adds one child chunk.
func (s *Segments) AddChildChunk(ctx context.Context, segmentID, content string) (map[string]any, error) {
	o, err := s.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: s.path("segments", port.PathEscape(segmentID), "child_chunks"), Body: map[string]any{"content": content}})
	if err != nil {
		return nil, err
	}
	return codec.UnwrapData(o), nil
}

// UpdateChildChunk changes one child chunk's content.
func (s *Segments) UpdateChildChunk(ctx context.Context, segmentID, chunkID, content string) (map[string]any, error) {
	o, err := s.api.Call(ctx, &port.Request{Method: http.MethodPatch, Path: s.path("segments", port.PathEscape(segmentID), "child_chunks", port.PathEscape(chunkID)), Body: map[string]any{"content": content}})
	if err != nil {
		return nil, err
	}
	return codec.UnwrapData(o), nil
}

// DeleteChildChunk removes one child chunk.
func (s *Segments) DeleteChildChunk(ctx context.Context, segmentID, chunkID string) error {
	_, err := s.api.Call(ctx, &port.Request{Method: http.MethodDelete, Path: s.path("segments", port.PathEscape(segmentID), "child_chunks", port.PathEscape(chunkID))})
	return err
}

package dify

import (
	"context"
	"net/http"
)

// Segment is a chunk of a document, as retrieval sees it.
type Segment struct {
	ID        string
	Content   string
	Answer    string
	Keywords  []string
	Position  int
	WordCount int
	Enabled   bool
	Raw       map[string]any
}

func (s *Segment) String() string { return s.Content }

func segmentFrom(o object) *Segment {
	return &Segment{
		ID:        o.str("id"),
		Content:   o.str("content"),
		Answer:    o.str("answer"),
		Keywords:  o.strs("keywords"),
		Position:  o.int("position"),
		WordCount: o.int("word_count"),
		Enabled:   boolOr(o, "enabled", true),
		Raw:       o.raw(),
	}
}

// Segments are the chunks of one document, and the child chunks beneath
// them.
type Segments struct {
	api        port
	datasetID  string
	documentID string
}

func (s *Segments) path(parts ...string) string {
	return datasetPath(s.datasetID, append([]string{"documents", pathEscape(s.documentID)}, parts...)...)
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
func (s *Segments) List(ctx context.Context, p *SegmentListParams) (*Page[*Segment], error) {
	if p == nil {
		p = &SegmentListParams{}
	}
	fetch := func(ctx context.Context, number int) (object, error) {
		q := params{}.set("keyword", p.Keyword).set("status", p.Status).setInt("page", number).setInt("limit", p.Limit)
		return s.api.call(ctx, &request{method: http.MethodGet, path: s.path("segments"), query: q.values()})
	}
	return fetchByPage(ctx, segmentFrom, fetch, p.Page)
}

// Retrieve reads one segment back.
func (s *Segments) Retrieve(ctx context.Context, segmentID string) (*Segment, error) {
	o, err := s.api.call(ctx, &request{method: http.MethodGet, path: s.path("segments", pathEscape(segmentID))})
	if err != nil {
		return nil, err
	}
	data := o.obj("data")
	if len(data) == 0 {
		data = o
	}
	return segmentFrom(data), nil
}

// Create adds segments by hand, rather than letting Dify chunk. Each map is
// {"content": ..., "answer": ..., "keywords": [...]}.
func (s *Segments) Create(ctx context.Context, segments []map[string]any) ([]*Segment, error) {
	o, err := s.api.call(ctx, &request{method: http.MethodPost, path: s.path("segments"), body: map[string]any{"segments": segments}})
	if err != nil {
		return nil, err
	}
	return buildAll(o.objs("data"), segmentFrom), nil
}

// Update changes one segment. fields is merged onto Dify's own
// "segment": {...} body — {"content": ..., "answer": ..., "keywords": [...],
// "enabled": ...}.
func (s *Segments) Update(ctx context.Context, segmentID string, fields map[string]any) (*Segment, error) {
	o, err := s.api.call(ctx, &request{method: http.MethodPost, path: s.path("segments", pathEscape(segmentID)), body: map[string]any{"segment": fields}})
	if err != nil {
		return nil, err
	}
	data := o.obj("data")
	if len(data) == 0 {
		data = o
	}
	return segmentFrom(data), nil
}

// Delete removes one segment.
func (s *Segments) Delete(ctx context.Context, segmentID string) error {
	_, err := s.api.call(ctx, &request{method: http.MethodDelete, path: s.path("segments", pathEscape(segmentID))})
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
	q := params{}.setInt("page", firstNonZero(p.Page, 1)).setInt("limit", firstNonZero(p.Limit, 20)).set("keyword", p.Keyword)
	o, err := s.api.call(ctx, &request{method: http.MethodGet, path: s.path("segments", pathEscape(segmentID), "child_chunks"), query: q.values()})
	if err != nil {
		return nil, err
	}
	return o.maps("data"), nil
}

// AddChildChunk adds one child chunk.
func (s *Segments) AddChildChunk(ctx context.Context, segmentID, content string) (map[string]any, error) {
	o, err := s.api.call(ctx, &request{method: http.MethodPost, path: s.path("segments", pathEscape(segmentID), "child_chunks"), body: map[string]any{"content": content}})
	if err != nil {
		return nil, err
	}
	return unwrapData(o), nil
}

// UpdateChildChunk changes one child chunk's content.
func (s *Segments) UpdateChildChunk(ctx context.Context, segmentID, chunkID, content string) (map[string]any, error) {
	o, err := s.api.call(ctx, &request{method: http.MethodPatch, path: s.path("segments", pathEscape(segmentID), "child_chunks", pathEscape(chunkID)), body: map[string]any{"content": content}})
	if err != nil {
		return nil, err
	}
	return unwrapData(o), nil
}

// DeleteChildChunk removes one child chunk.
func (s *Segments) DeleteChildChunk(ctx context.Context, segmentID, chunkID string) error {
	_, err := s.api.call(ctx, &request{method: http.MethodDelete, path: s.path("segments", pathEscape(segmentID), "child_chunks", pathEscape(chunkID))})
	return err
}

// unwrapData is the "data" object inside an envelope, or the envelope itself
// when there is no such wrapping.
func unwrapData(o object) map[string]any {
	if data := o.obj("data"); len(data) > 0 {
		return data.raw()
	}
	return o.raw()
}

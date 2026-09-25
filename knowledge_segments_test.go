package dify

import (
	"context"
	"net/http"
	"testing"
)

// TestEverySegmentVerbReachesItsRoute pins each Segments verb to the method
// and path Dify's controller defines, including the child-chunk routes,
// which keep the underscored "child_chunks" segment because that is what
// Dify's own route uses — not every underscore in this API is deprecated.
func TestEverySegmentVerbReachesItsRoute(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		call   func(s *Segments) error
		method string
		path   string
		body   map[string]any
	}{
		{"list", func(s *Segments) error { _, err := s.List(ctx, nil); return err }, "GET", "/v1/datasets/d1/documents/doc1/segments", nil},
		{"retrieve", func(s *Segments) error { _, err := s.Retrieve(ctx, "seg1"); return err }, "GET", "/v1/datasets/d1/documents/doc1/segments/seg1", nil},
		{"create", func(s *Segments) error {
			_, err := s.Create(ctx, []map[string]any{{"content": "hello"}})
			return err
		}, "POST", "/v1/datasets/d1/documents/doc1/segments", map[string]any{"segments": []any{map[string]any{"content": "hello"}}}},
		{"update", func(s *Segments) error {
			_, err := s.Update(ctx, "seg1", map[string]any{"content": "updated"})
			return err
		}, "POST", "/v1/datasets/d1/documents/doc1/segments/seg1", map[string]any{"segment": map[string]any{"content": "updated"}}},
		{"delete", func(s *Segments) error { return s.Delete(ctx, "seg1") }, "DELETE", "/v1/datasets/d1/documents/doc1/segments/seg1", nil},
		{"child chunks list", func(s *Segments) error { _, err := s.ChildChunks(ctx, "seg1", nil); return err }, "GET", "/v1/datasets/d1/documents/doc1/segments/seg1/child_chunks", nil},
		{"add child chunk", func(s *Segments) error {
			_, err := s.AddChildChunk(ctx, "seg1", "child text")
			return err
		}, "POST", "/v1/datasets/d1/documents/doc1/segments/seg1/child_chunks", map[string]any{"content": "child text"}},
		{"update child chunk", func(s *Segments) error {
			_, err := s.UpdateChildChunk(ctx, "seg1", "c1", "new text")
			return err
		}, "PATCH", "/v1/datasets/d1/documents/doc1/segments/seg1/child_chunks/c1", map[string]any{"content": "new text"}},
		{"delete child chunk", func(s *Segments) error { return s.DeleteChildChunk(ctx, "seg1", "c1") }, "DELETE", "/v1/datasets/d1/documents/doc1/segments/seg1/child_chunks/c1", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, 200, map[string]any{"data": map[string]any{"id": "seg1", "content": "hello"}})
			})
			s := knowledgeClient(t, f).Documents("d1").Segments("doc1")
			if err := tc.call(s); err != nil {
				t.Fatal(err)
			}
			got := f.last(t)
			if got.Method != tc.method || got.Path != tc.path {
				t.Fatalf("want %s %s, got %s %s", tc.method, tc.path, got.Method, got.Path)
			}
			for k, v := range tc.body {
				if list, ok := v.([]any); ok {
					gotList, _ := got.Body[k].([]any)
					if len(gotList) != len(list) {
						t.Errorf("body[%s] = %v, want length %d", k, got.Body[k], len(list))
					}
					continue
				}
				if m, ok := v.(map[string]any); ok {
					gotMap, _ := got.Body[k].(map[string]any)
					for mk, mv := range m {
						if gotMap[mk] != mv {
							t.Errorf("body[%s][%s] = %v, want %v", k, mk, gotMap[mk], mv)
						}
					}
					continue
				}
				if got.Body[k] != v {
					t.Errorf("body[%s] = %v, want %v (body %v)", k, got.Body[k], v, got.Body)
				}
			}
		})
	}
}

// knowledgeFirstQueryValue is the first value of a query parameter recorded
// by fakeDify, or "" when it was not sent.
func knowledgeFirstQueryValue(q map[string][]string, key string) string {
	if v := q[key]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// TestChildChunkListDefaultsPageAndLimit pins Dify's own defaults (page 1,
// limit 20) for the one listing in this API with no has_more envelope to
// fall back on.
func TestChildChunkListDefaultsPageAndLimit(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"data": []any{}}) })
	s := knowledgeClient(t, f).Documents("d1").Segments("doc1")
	if _, err := s.ChildChunks(context.Background(), "seg1", nil); err != nil {
		t.Fatal(err)
	}
	q := f.last(t).Query
	page, limit := knowledgeFirstQueryValue(q, "page"), knowledgeFirstQueryValue(q, "limit")
	if page != "1" || limit != "20" {
		t.Fatalf("got page=%s limit=%s", page, limit)
	}
}

// TestAddChildChunkUnwrapsTheDataEnvelope pins a deliberate deviation from
// the Python SDK: add_child_chunk/update_child_chunk there return the whole
// {"data": {...}} envelope verbatim while every other Segments method
// unwraps "data" — an inconsistency in the original, not a Dify contract.
// This SDK unwraps consistently.
func TestAddChildChunkUnwrapsTheDataEnvelope(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"data": map[string]any{"id": "c1", "content": "child text"}})
	})
	s := knowledgeClient(t, f).Documents("d1").Segments("doc1")
	got, err := s.AddChildChunk(context.Background(), "seg1", "child text")
	if err != nil {
		t.Fatal(err)
	}
	if got["id"] != "c1" {
		t.Fatalf("got %v, want the unwrapped child chunk", got)
	}
}

// TestSegmentEnabledDefaultsTrueWhenAbsent mirrors the same rule as
// documents: a segment with no "enabled" field is enabled.
func TestSegmentEnabledDefaultsTrueWhenAbsent(t *testing.T) {
	seg := segmentFrom(object{"id": "s1"})
	if !seg.Enabled {
		t.Fatal("want enabled to default true")
	}
}

// TestSegmentListingWalksPagesUntilHasMoreIsFalse pins the same paging
// contract Datasets.List has, since Segments.List answers with the same
// has_more/limit/total envelope.
func TestSegmentListingWalksPagesUntilHasMoreIsFalse(t *testing.T) {
	pages := [][]any{
		{map[string]any{"id": "s1"}},
		{map[string]any{"id": "s2"}},
	}
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		i := 0
		if r.URL.Query().Get("page") == "2" {
			i = 1
		}
		writeJSON(w, 200, map[string]any{"data": pages[i], "has_more": i == 0, "limit": 1, "total": 2, "page": i + 1})
	})
	s := knowledgeClient(t, f).Documents("d1").Segments("doc1")
	page, err := s.List(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	all, err := page.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].ID != "s1" || all[1].ID != "s2" {
		t.Fatalf("got %+v", all)
	}
}

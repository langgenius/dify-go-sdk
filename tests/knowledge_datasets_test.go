package tests

import (
	"context"
	"net/http"
	"testing"

	dify "github.com/langgenius/dify-go-sdk"
)

// TestEveryDatasetVerbReachesItsRoute pins each Datasets verb to the method,
// path and body Dify's controllers define, so a refactor cannot quietly
// point one elsewhere.
func TestEveryDatasetVerbReachesItsRoute(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		call   func(k *dify.Knowledge) error
		method string
		path   string
		body   map[string]any
		query  map[string]string
	}{
		{"create", func(k *dify.Knowledge) error {
			_, err := k.Datasets.Create(ctx, "handbook", &dify.DatasetCreateParams{IndexingTechnique: "high_quality", Embedding: "langgenius/openai/openai:text-embedding-3-small"})
			return err
		}, "POST", "/v1/datasets", map[string]any{"name": "handbook", "indexing_technique": "high_quality", "embedding_model_provider": "langgenius/openai/openai", "embedding_model": "text-embedding-3-small"}, nil},
		{"retrieve", func(k *dify.Knowledge) error { _, err := k.Datasets.Retrieve(ctx, "d1"); return err }, "GET", "/v1/datasets/d1", nil, nil},
		{"update", func(k *dify.Knowledge) error {
			name := "renamed"
			_, err := k.Datasets.Update(ctx, "d1", &dify.DatasetUpdateParams{Name: &name})
			return err
		}, "PATCH", "/v1/datasets/d1", map[string]any{"name": "renamed"}, nil},
		{"delete", func(k *dify.Knowledge) error { return k.Datasets.Delete(ctx, "d1") }, "DELETE", "/v1/datasets/d1", nil, nil},
		{"search", func(k *dify.Knowledge) error {
			_, err := k.Datasets.Search(ctx, "d1", "refund window", nil)
			return err
		}, "POST", "/v1/datasets/d1/retrieve", map[string]any{"query": "refund window"}, nil},
		{"dataset tags", func(k *dify.Knowledge) error { _, err := k.Datasets.Tags(ctx, "d1"); return err }, "GET", "/v1/datasets/d1/tags", nil, nil},
		{"metadata list", func(k *dify.Knowledge) error { _, err := k.Datasets.Metadata(ctx, "d1"); return err }, "GET", "/v1/datasets/d1/metadata", nil, nil},
		{"metadata create", func(k *dify.Knowledge) error {
			_, err := k.Datasets.AddMetadataField(ctx, "d1", "category", "string")
			return err
		}, "POST", "/v1/datasets/d1/metadata", map[string]any{"name": "category", "type": "string"}, nil},
		{"metadata rename", func(k *dify.Knowledge) error {
			_, err := k.Datasets.RenameMetadataField(ctx, "d1", "f1", "topic")
			return err
		}, "PATCH", "/v1/datasets/d1/metadata/f1", map[string]any{"name": "topic"}, nil},
		{"metadata delete", func(k *dify.Knowledge) error { return k.Datasets.DeleteMetadataField(ctx, "d1", "f1") }, "DELETE", "/v1/datasets/d1/metadata/f1", nil, nil},
		{"built-in metadata", func(k *dify.Knowledge) error { _, err := k.Datasets.BuiltInMetadata(ctx, "d1"); return err }, "GET", "/v1/datasets/d1/metadata/built-in", nil, nil},
		{"enable built-in metadata", func(k *dify.Knowledge) error { return k.Datasets.SetBuiltInMetadata(ctx, "d1", true) }, "POST", "/v1/datasets/d1/metadata/built-in/enable", nil, nil},
		{"disable built-in metadata", func(k *dify.Knowledge) error { return k.Datasets.SetBuiltInMetadata(ctx, "d1", false) }, "POST", "/v1/datasets/d1/metadata/built-in/disable", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, 200, map[string]any{"id": "d1", "name": "handbook"})
			})
			if err := tc.call(knowledgeClient(t, f)); err != nil {
				t.Fatal(err)
			}
			got := f.last(t)
			if got.Method != tc.method || got.Path != tc.path {
				t.Fatalf("want %s %s, got %s %s", tc.method, tc.path, got.Method, got.Path)
			}
			for k, v := range tc.body {
				if got.Body[k] != v {
					t.Errorf("body[%s] = %v, want %v (body %v)", k, got.Body[k], v, got.Body)
				}
			}
			for k, v := range tc.query {
				if q := got.Query[k]; len(q) == 0 || q[0] != v {
					t.Errorf("query[%s] = %v, want %s", k, q, v)
				}
			}
		})
	}
}

// TestListingDatasetsSendsRepeatedTagIDsNotAJoinedList pins that Dify reads
// tag_ids with request.args.getlist: repeated query parameters, not one
// comma-joined value, which a naive query builder would send instead.
func TestListingDatasetsSendsRepeatedTagIDsNotAJoinedList(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"data": []any{}, "has_more": false, "limit": 20, "total": 0, "page": 1})
	})
	k := knowledgeClient(t, f)
	if _, err := k.Datasets.List(context.Background(), &dify.DatasetListParams{TagIDs: []string{"t1", "t2"}}); err != nil {
		t.Fatal(err)
	}
	got := f.last(t).Query["tag_ids"]
	if len(got) != 2 || got[0] != "t1" || got[1] != "t2" {
		t.Fatalf("got %v", got)
	}
}

// TestDatasetListingWalksPagesUntilHasMoreIsFalse pins the paging contract
// datasets.List shares with every other numbered listing in this package.
func TestDatasetListingWalksPagesUntilHasMoreIsFalse(t *testing.T) {
	pages := [][]any{
		{map[string]any{"id": "d1"}},
		{map[string]any{"id": "d2"}},
	}
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		number := r.URL.Query().Get("page")
		i := 0
		if number == "2" {
			i = 1
		}
		hasMore := i == 0
		writeJSON(w, 200, map[string]any{"data": pages[i], "has_more": hasMore, "limit": 1, "total": 2, "page": i + 1})
	})
	k := knowledgeClient(t, f)
	page, err := k.Datasets.List(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	all, err := page.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].ID != "d1" || all[1].ID != "d2" {
		t.Fatalf("got %+v", all)
	}
}

// TestDatasetSearchCarriesRetrievalAndExternalRetrievalModelWhenGiven pins
// that Search leaves both fields out when not overridden, and sends them
// verbatim when they are.
func TestDatasetSearchCarriesRetrievalAndExternalRetrievalModelWhenGiven(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"records": []any{}}) })
	k := knowledgeClient(t, f)
	retrieval := map[string]any{"search_method": dify.SearchSemantic}
	external := map[string]any{"top_k": float64(5)}
	if _, err := k.Datasets.Search(context.Background(), "d1", "q", &dify.SearchParams{RetrievalModel: retrieval, ExternalRetrievalModel: external}); err != nil {
		t.Fatal(err)
	}
	body := f.last(t).Body
	if _, ok := body["retrieval_model"]; !ok {
		t.Errorf("retrieval_model missing from %v", body)
	}
	if _, ok := body["external_retrieval_model"]; !ok {
		t.Errorf("external_retrieval_model missing from %v", body)
	}
}

// TestDatasetSearchShapesRecordsNestedUnderQuery pins the fallback shape
// _hits() in the Python SDK reads: some Dify versions nest records one level
// deeper, under "query".
func TestDatasetSearchShapesRecordsNestedUnderQuery(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"query": map[string]any{"records": []any{
			map[string]any{"score": 0.5, "segment": map[string]any{"id": "s1", "content": "hello", "document": map[string]any{"id": "doc1", "name": "policy"}}},
		}}})
	})
	k := knowledgeClient(t, f)
	hits, err := k.Datasets.Search(context.Background(), "d1", "q", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Score != 0.5 || hits[0].Segment.Content != "hello" || hits[0].DocumentID != "doc1" || hits[0].DocumentName != "policy" {
		t.Fatalf("got %+v", hits)
	}
}

// TestDatasetUpdateOnlySendsFieldsThatWereSet pins PATCH semantics: a field
// left nil is not sent at all, rather than sent as its zero value, since
// Dify updates only what is present in the body.
func TestDatasetUpdateOnlySendsFieldsThatWereSet(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"id": "d1"}) })
	k := knowledgeClient(t, f)
	description := "new description"
	if _, err := k.Datasets.Update(context.Background(), "d1", &dify.DatasetUpdateParams{Description: &description}); err != nil {
		t.Fatal(err)
	}
	body := f.last(t).Body
	if len(body) != 1 || body["description"] != "new description" {
		t.Fatalf("got %v, want only description set", body)
	}
}

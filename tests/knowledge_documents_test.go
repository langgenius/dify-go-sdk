package tests

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	dify "github.com/langgenius/dify-go-sdk"
	"github.com/langgenius/dify-go-sdk/internal/codec"
	"github.com/langgenius/dify-go-sdk/internal/kernel"
)

// TestEveryDocumentVerbReachesItsRoute pins each Documents verb to Dify's
// canonical hyphenated routes — never the deprecated underscored aliases, and
// never a spelling of update-by-file, since PATCH /documents/<id> replaces
// both of those.
func TestEveryDocumentVerbReachesItsRoute(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		call   func(docs *dify.Documents) error
		method string
		path   string
		body   map[string]any
	}{
		{"create from text", func(docs *dify.Documents) error {
			_, err := docs.CreateFromText(ctx, "policy", "refunds within 30 days", nil)
			return err
		}, "POST", "/v1/datasets/d1/document/create-by-text", map[string]any{"name": "policy", "text": "refunds within 30 days", "indexing_technique": "high_quality"}},
		{"list", func(docs *dify.Documents) error { _, err := docs.List(ctx, nil); return err }, "GET", "/v1/datasets/d1/documents", nil},
		{"update from text", func(docs *dify.Documents) error {
			_, err := docs.UpdateFromText(ctx, "doc1", "new text", &dify.DocumentUpdateTextParams{Name: "policy"})
			return err
		}, "POST", "/v1/datasets/d1/documents/doc1/update-by-text", map[string]any{"text": "new text", "name": "policy"}},
		{"retrieve", func(docs *dify.Documents) error { _, err := docs.Retrieve(ctx, "doc1"); return err }, "GET", "/v1/datasets/d1/documents/doc1", nil},
		{"delete", func(docs *dify.Documents) error { return docs.Delete(ctx, "doc1") }, "DELETE", "/v1/datasets/d1/documents/doc1", nil},
		{"indexing status", func(docs *dify.Documents) error { _, err := docs.IndexingStatus(ctx, "batch1"); return err }, "GET", "/v1/datasets/d1/documents/batch1/indexing-status", nil},
		{"set status enable", func(docs *dify.Documents) error { return docs.SetEnabled(ctx, []string{"doc1"}, true) }, "PATCH", "/v1/datasets/d1/documents/status/enable", map[string]any{"document_ids": []any{"doc1"}}},
		{"set status archive", func(docs *dify.Documents) error { return docs.SetStatus(ctx, []string{"doc1"}, dify.DocumentArchive) }, "PATCH", "/v1/datasets/d1/documents/status/archive", map[string]any{"document_ids": []any{"doc1"}}},
		{"download url", func(docs *dify.Documents) error { _, err := docs.DownloadURL(ctx, "doc1"); return err }, "GET", "/v1/datasets/d1/documents/doc1/download", nil},
		{"download all", func(docs *dify.Documents) error {
			_, err := docs.DownloadAll(ctx, []string{"doc1", "doc2"})
			return err
		}, "POST", "/v1/datasets/d1/documents/download-zip", map[string]any{"document_ids": []any{"doc1", "doc2"}}},
		{"set metadata", func(docs *dify.Documents) error {
			return docs.SetMetadata(ctx, []map[string]any{{"document_id": "doc1", "metadata_list": []any{}}})
		}, "POST", "/v1/datasets/d1/documents/metadata", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, 200, map[string]any{"document": map[string]any{"id": "doc1", "name": "policy"}, "batch": "batch1"})
			})
			docs := knowledgeClient(t, f).Documents("d1")
			if err := tc.call(docs); err != nil {
				t.Fatal(err)
			}
			got := f.last(t)
			if got.Method != tc.method || got.Path != tc.path {
				t.Fatalf("want %s %s, got %s %s", tc.method, tc.path, got.Method, got.Path)
			}
			for k, v := range tc.body {
				gotV, ok := got.Body[k]
				if !ok {
					t.Errorf("body missing key %s (body %v)", k, got.Body)
					continue
				}
				if list, ok := v.([]any); ok {
					gotList, _ := gotV.([]any)
					if len(gotList) != len(list) {
						t.Errorf("body[%s] = %v, want %v", k, gotV, v)
					}
					continue
				}
				if gotV != v {
					t.Errorf("body[%s] = %v, want %v (body %v)", k, gotV, v, got.Body)
				}
			}
		})
	}
}

// TestDeprecatedDocumentRoutesAreNeverUsed pins that this SDK only ever
// speaks the hyphenated canonical routes: DeprecatedDocumentAddByTextApi and
// friends are Dify's underscored aliases for the same operations.
func TestDeprecatedDocumentRoutesAreNeverUsed(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"document": map[string]any{"id": "doc1"}, "batch": "b1"})
	})
	docs := knowledgeClient(t, f).Documents("d1")
	if _, err := docs.CreateFromText(context.Background(), "name", "text", nil); err != nil {
		t.Fatal(err)
	}
	if got := f.last(t).Path; strings.Contains(got, "create_by_text") {
		t.Fatalf("used the deprecated route: %s", got)
	}
}

// TestCreateFromFileSendsSettingsAsAMultipartDataField pins the multipart
// shape create-by-file wants: the JSON settings under a "data" field beside
// the file, not as the request body.
func TestCreateFromFileSendsSettingsAsAMultipartDataField(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"document": map[string]any{"id": "doc1"}, "batch": "b1"})
	})
	docs := knowledgeClient(t, f).Documents("d1")
	upload := dify.FileFromReader("handbook.txt", strings.NewReader("hello"))
	if _, err := docs.CreateFromFile(context.Background(), upload, nil); err != nil {
		t.Fatal(err)
	}
	got := f.last(t)
	if got.Method != "POST" || got.Path != "/v1/datasets/d1/document/create-by-file" {
		t.Fatalf("got %s %s", got.Method, got.Path)
	}
	raw := string(got.Raw)
	if !strings.Contains(raw, `name="data"`) || !strings.Contains(raw, `"indexing_technique":"high_quality"`) {
		t.Errorf("settings not carried in the data field: %s", raw)
	}
	if !strings.Contains(raw, `filename="handbook.txt"`) {
		t.Errorf("file part missing its name: %s", raw)
	}
}

// TestUpdateFromFileHasNoEmbeddingFieldOnUpdateByTextButDoesOnUpdateByFile
// pins a controller detail the Python SDK's shared **extra kwargs blur:
// DocumentTextUpdate has no embedding_model field, but the update-by-file
// settings do — so switching the embedding model is only a thing the file
// path can do.
func TestUpdateFromFileCarriesEmbeddingSettingsInTheDataField(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"document": map[string]any{"id": "doc1"}, "batch": "b2"})
	})
	docs := knowledgeClient(t, f).Documents("d1")
	upload := dify.FileFromReader("handbook.txt", strings.NewReader("hello"))
	_, err := docs.UpdateFromFile(context.Background(), "doc1", upload, &dify.DocumentUpdateFileParams{Embedding: "langgenius/openai/openai:text-embedding-3-small"})
	if err != nil {
		t.Fatal(err)
	}
	got := f.last(t)
	if got.Method != "PATCH" || got.Path != "/v1/datasets/d1/documents/doc1" {
		t.Fatalf("got %s %s", got.Method, got.Path)
	}
	if !strings.Contains(string(got.Raw), `"embedding_model":"text-embedding-3-small"`) {
		t.Errorf("embedding settings not carried: %s", got.Raw)
	}
}

// TestUpdateFromTextFetchesTheCurrentNameWhenNoneIsGiven pins the same trick
// the Python SDK uses: Dify requires a name alongside text and answers a
// bare validation error rather than saying which field it wants, so the
// current document's name is fetched rather than surfacing that.
func TestUpdateFromTextFetchesTheCurrentNameWhenNoneIsGiven(t *testing.T) {
	calls := 0
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method == "GET" {
			writeJSON(w, 200, map[string]any{"id": "doc1", "name": "existing-name"})
			return
		}
		writeJSON(w, 200, map[string]any{"document": map[string]any{"id": "doc1", "name": "existing-name"}, "batch": "b3"})
	})
	docs := knowledgeClient(t, f).Documents("d1")
	if _, err := docs.UpdateFromText(context.Background(), "doc1", "new text", nil); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("want a GET to fetch the name then the POST, got %d calls", calls)
	}
	got := f.last(t)
	if got.Body["name"] != "existing-name" {
		t.Fatalf("got %v", got.Body)
	}
}

// TestCreatedDocumentCarriesTheIndexingBatchAlongside pins the shape Dify
// answers a create/update with: the document nested under "document", and
// the batch beside it rather than inside it — dropping this makes
// IndexingStatus and WaitUntilIndexed unusable on a just-created document.
func TestCreatedDocumentCarriesTheIndexingBatchAlongside(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"document": map[string]any{"id": "doc1", "name": "policy"}, "batch": "the-batch"})
	})
	docs := knowledgeClient(t, f).Documents("d1")
	doc, err := docs.CreateFromText(context.Background(), "policy", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if doc.ID != "doc1" || doc.Batch != "the-batch" {
		t.Fatalf("got %+v", doc)
	}
}

// TestDocumentEnabledDefaultsTrueWhenAbsent pins Dify's own default: a
// document with no "enabled" field at all is enabled, not disabled.
func TestDocumentEnabledDefaultsTrueWhenAbsent(t *testing.T) {
	doc := codec.DocumentFrom(kernel.Object{"id": "doc1"})
	if !doc.Enabled {
		t.Fatal("want enabled to default true when the field is absent")
	}
	disabled := codec.DocumentFrom(kernel.Object{"id": "doc1", "enabled": false})
	if disabled.Enabled {
		t.Fatal("want an explicit false to be honoured")
	}
}

// TestIndexingStatusRefusesAnEmptyBatch pins that this client will not spend
// a request on a call Dify would answer meaninglessly: a document read back
// from List or Retrieve carries no batch to ask about.
func TestIndexingStatusRefusesAnEmptyBatch(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("should not have sent a request") })
	docs := knowledgeClient(t, f).Documents("d1")
	_, err := docs.IndexingStatus(context.Background(), "")
	if !errors.Is(err, dify.ErrValidation) {
		t.Fatalf("got %v, want ErrValidation", err)
	}
}

// TestWaitUntilIndexedSucceedsOnceIndexingCompletes pins the success path:
// polling stops the moment Dify reports "completed".
func TestWaitUntilIndexedSucceedsOnceIndexingCompletes(t *testing.T) {
	calls := 0
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		status := "indexing"
		if calls >= 3 {
			status = "completed"
		}
		writeJSON(w, 200, map[string]any{"data": []any{map[string]any{"id": "batch1", "indexing_status": status, "completed_segments": calls, "total_segments": 3}}})
	})
	docs := knowledgeClient(t, f).Documents("d1")
	status, err := docs.WaitUntilIndexed(context.Background(), "batch1", &dify.WaitParams{Poll: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if !status.Indexed() || calls < 3 {
		t.Fatalf("got %+v after %d calls", status, calls)
	}
}

// TestWaitUntilIndexedNamesTheDocumentWhenIndexingErrors pins that indexing
// stopping at "error" (or "paused") raises rather than returning success —
// it used to return, so a caller went on to search a knowledge base that had
// silently indexed nothing.
func TestWaitUntilIndexedNamesTheDocumentWhenIndexingErrors(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"data": []any{map[string]any{"id": "batch1", "indexing_status": "error", "error": "out of quota"}}})
	})
	docs := knowledgeClient(t, f).Documents("d1")
	status, err := docs.WaitUntilIndexed(context.Background(), "batch1", &dify.WaitParams{Poll: time.Millisecond})
	if err == nil {
		t.Fatal("want an error when indexing stopped at error")
	}
	if !strings.Contains(err.Error(), "batch1") || !strings.Contains(err.Error(), "out of quota") {
		t.Fatalf("error does not name the batch/detail: %v", err)
	}
	if status == nil || status.Status != "error" {
		t.Fatalf("got %+v", status)
	}
}

// TestWaitUntilIndexedStopsWhenTheContextIsCancelled pins that a caller's own
// cancellation is respected between polls rather than being retried away.
func TestWaitUntilIndexedStopsWhenTheContextIsCancelled(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"data": []any{map[string]any{"id": "batch1", "indexing_status": "indexing"}}})
	})
	docs := knowledgeClient(t, f).Documents("d1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := docs.WaitUntilIndexed(ctx, "batch1", &dify.WaitParams{Poll: time.Millisecond})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

// TestWaitUntilSettledReturnsAPausedStatusWithoutError pins the distinction
// AGENTS.md draws between wait_until_settled and wait_until_indexed: the
// former reports the outcome, including a pause, without treating it as a
// failure.
func TestWaitUntilSettledReturnsAPausedStatusWithoutError(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"data": []any{map[string]any{"id": "batch1", "indexing_status": "paused"}}})
	})
	docs := knowledgeClient(t, f).Documents("d1")
	status, err := docs.WaitUntilSettled(context.Background(), "batch1", &dify.WaitParams{Poll: time.Millisecond})
	if err != nil {
		t.Fatalf("WaitUntilSettled should not error on a pause: %v", err)
	}
	if status.Status != "paused" || status.Indexed() {
		t.Fatalf("got %+v", status)
	}
}

// TestDownloadURLReadsTheSignedURLRatherThanBytes pins a discrepancy from the
// Python SDK: Dify's document-download route answers with {"url": "..."}, a
// signed URL, not the file's bytes — the Python SDK reads the response body
// as if it were the file itself, which on this route is the JSON envelope's
// raw text rather than a document.
func TestDownloadURLReadsTheSignedURLRatherThanBytes(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"url": "https://files.example.com/signed/doc1?sign=abc"})
	})
	docs := knowledgeClient(t, f).Documents("d1")
	url, err := docs.DownloadURL(context.Background(), "doc1")
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://files.example.com/signed/doc1?sign=abc" {
		t.Fatalf("got %q", url)
	}
}

// TestDownloadAllRefusesAnEmptyDocumentList pins that Dify has no
// download-everything: an empty selection is refused before spending a
// request rather than downloading nothing usable.
func TestDownloadAllRefusesAnEmptyDocumentList(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("should not have sent a request") })
	docs := knowledgeClient(t, f).Documents("d1")
	if _, err := docs.DownloadAll(context.Background(), nil); !errors.Is(err, dify.ErrValidation) {
		t.Fatalf("got %v, want ErrValidation", err)
	}
}

package tests

import (
	"context"
	"net/http"
	"strings"
	"testing"

	dify "github.com/langgenius/dify-go-sdk"
)

// TestEveryPipelineVerbReachesItsRoute pins each Pipeline verb to the method,
// path and body Dify's controller defines.
func TestEveryPipelineVerbReachesItsRoute(t *testing.T) {
	ctx := context.Background()
	in := dify.PipelineRunInput{StartNodeID: "start1", DatasourceType: "upload_file", DatasourceInfoList: []map[string]any{{"file_ids": []any{"f1"}}}}
	cases := []struct {
		name   string
		call   func(pl *dify.Pipeline) error
		method string
		path   string
		body   map[string]any
	}{
		{"datasources", func(pl *dify.Pipeline) error { _, err := pl.Datasources(ctx, nil); return err }, "GET", "/v1/datasets/d1/pipeline/datasource-plugins", nil},
		{"run datasource node", func(pl *dify.Pipeline) error {
			_, err := pl.RunDatasourceNode(ctx, "node1", "upload_file", map[string]any{"a": 1}, nil)
			return err
		}, "POST", "/v1/datasets/d1/pipeline/datasource/nodes/node1/run", map[string]any{"datasource_type": "upload_file", "is_published": true}},
		{"run", func(pl *dify.Pipeline) error { _, err := pl.Run(ctx, in); return err }, "POST", "/v1/datasets/d1/pipeline/run", map[string]any{"is_published": true, "response_mode": "blocking", "start_node_id": "start1"}},
		{"run draft", func(pl *dify.Pipeline) error { _, err := pl.RunDraft(ctx, in); return err }, "POST", "/v1/datasets/d1/pipeline/run", map[string]any{"is_published": false, "response_mode": "blocking"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, 200, map[string]any{"batch": "b1", "dataset": map[string]any{"id": "d1"}, "documents": []any{}, "data": map[string]any{"status": "succeeded"}})
			})
			pl := knowledgeClient(t, f).Pipeline("d1")
			if err := tc.call(pl); err != nil {
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
		})
	}
}

// TestPublishedPipelineRunReturnsAnIngestionNotAWorkflowRun pins the
// distinction AGENTS.md draws: a published run is queued and answers with a
// batch and the documents it queued, never a WorkflowRun.
func TestPublishedPipelineRunReturnsAnIngestionNotAWorkflowRun(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"batch":   "batch1",
			"dataset": map[string]any{"id": "d1"},
			"documents": []any{
				map[string]any{"id": "doc1", "name": "source.pdf", "indexing_status": "queuing"},
			},
		})
	})
	pl := knowledgeClient(t, f).Pipeline("d1")
	in := dify.PipelineRunInput{StartNodeID: "start1", DatasourceType: "upload_file", DatasourceInfoList: []map[string]any{{"file_ids": []any{"f1"}}}}
	ingestion, err := pl.Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if ingestion.Batch != "batch1" || ingestion.DatasetID != "d1" || len(ingestion.Documents) != 1 {
		t.Fatalf("got %+v", ingestion)
	}
	if ingestion.Documents[0].Batch != "batch1" {
		t.Fatalf("the queued document should carry the shared batch, got %+v", ingestion.Documents[0])
	}
}

// TestDraftPipelineRunReturnsAWorkflowRun pins the other half of that
// distinction: a draft run executes the graph and reports a WorkflowRun,
// exactly like App.Workflows.Runs.Create does.
func TestDraftPipelineRunReturnsAWorkflowRun(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"data": map[string]any{"id": "run1", "status": "succeeded", "outputs": map[string]any{"ok": true}}})
	})
	pl := knowledgeClient(t, f).Pipeline("d1")
	in := dify.PipelineRunInput{StartNodeID: "start1", DatasourceType: "upload_file", DatasourceInfoList: []map[string]any{{"file_ids": []any{"f1"}}}}
	run, err := pl.RunDraft(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if run.RunID != "run1" || !run.Succeeded() {
		t.Fatalf("got %+v", run)
	}
}

// TestPipelineNotFoundIsRewordedAsAnAbsence pins that Dify's flat "Pipeline
// not found" answer — which every pipeline route gives an ordinary knowledge
// base, and which reads like a bug — is turned into an error naming the
// dataset and explaining that not every knowledge base has a pipeline.
func TestPipelineNotFoundIsRewordedAsAnAbsence(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 404, map[string]any{"message": "Pipeline not found."})
	})
	pl := knowledgeClient(t, f).Pipeline("d1")
	_, err := pl.Datasources(context.Background(), nil)
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "d1") || !strings.Contains(err.Error(), "no RAG pipeline") {
		t.Fatalf("got %v", err)
	}
}

// TestOtherPipelineErrorsAreNotReworded pins that only "Pipeline not found"
// is translated — an unrelated failure is left as Dify reported it.
func TestOtherPipelineErrorsAreNotReworded(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 500, map[string]any{"message": "internal server error"})
	})
	pl := knowledgeClient(t, f).Pipeline("d1")
	_, err := pl.Datasources(context.Background(), nil)
	if err == nil || strings.Contains(err.Error(), "no RAG pipeline") {
		t.Fatalf("got %v, want the original error left alone", err)
	}
}

// TestUploadForPipelineRefusesAFilenameWithNoExtension pins that Dify picks
// its reader by the file's extension and refuses a name without one; without
// this check, the upload succeeds and the document fails indexing instead,
// which is a worse place to discover the mistake.
func TestUploadForPipelineRefusesAFilenameWithNoExtension(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("should not have sent a request") })
	k := knowledgeClient(t, f)
	upload := dify.FileFromReader("handbook", strings.NewReader("hello"))
	_, err := k.UploadForPipeline(context.Background(), upload)
	if err == nil || !strings.Contains(err.Error(), "extension") {
		t.Fatalf("got %v", err)
	}
}

// TestUploadForPipelineSendsTheFileAsMultipart pins the route and that a
// properly named upload is sent, at the workspace level rather than scoped
// to one pipeline.
func TestUploadForPipelineSendsTheFileAsMultipart(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 201, map[string]any{"id": "file1", "name": "handbook.pdf"})
	})
	k := knowledgeClient(t, f)
	upload := dify.FileFromReader("handbook.pdf", strings.NewReader("hello"))
	got, err := k.UploadForPipeline(context.Background(), upload)
	if err != nil {
		t.Fatal(err)
	}
	if got["id"] != "file1" {
		t.Fatalf("got %v", got)
	}
	last := f.last(t)
	if last.Method != "POST" || last.Path != "/v1/datasets/pipeline/file-upload" {
		t.Fatalf("got %s %s", last.Method, last.Path)
	}
}

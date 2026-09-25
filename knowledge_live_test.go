package dify

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestKnowledgeLiveDatasetDocumentRoundTrip creates a real dataset, adds a
// text document, waits for it to index, retrieves it and deletes the
// dataset — checking the claims in this file against a running Dify rather
// than a mock, the same way live_test.go does for App.
//
// Gated on DIFY_HOST and DIFY_DATASET_API_KEY rather than the console-login
// harness in live_test.go: a dataset key is reveal-once and a workspace
// holds only ten, so this test takes one it is handed rather than minting
// its own.
func TestKnowledgeLiveDatasetDocumentRoundTrip(t *testing.T) {
	host := strings.TrimRight(os.Getenv(EnvHost), "/")
	key := os.Getenv(EnvDatasetAPIKey)
	if host == "" || key == "" {
		t.Skip("no live Dify configured: set DIFY_HOST and DIFY_DATASET_API_KEY to run this test")
	}

	k, err := NewKnowledge(WithAPIKey(key), WithBaseURL(host+"/v1"), WithUser(harnessPrefix))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	dataset, err := k.Datasets.Create(ctx, harnessPrefix+"-"+time.Now().UTC().Format("20060102-150405"), &DatasetCreateParams{IndexingTechnique: "high_quality"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := k.Datasets.Delete(context.Background(), dataset.ID); err != nil {
			t.Logf("live harness: could not delete dataset %s: %v", dataset.ID, err)
		}
	})

	docs := k.Documents(dataset.ID)
	doc, err := docs.CreateFromText(ctx, "handbook", "Refunds are available within 30 days of purchase.", nil)
	if err != nil {
		t.Fatal(err)
	}

	status, err := docs.WaitUntilIndexed(ctx, doc.Batch, &WaitParams{Timeout: 2 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if !status.Indexed() {
		t.Fatalf("got %+v", status)
	}

	back, err := docs.Retrieve(ctx, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.ID != doc.ID || !back.Indexed() {
		t.Fatalf("got %+v", back)
	}
}

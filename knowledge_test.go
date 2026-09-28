package dify

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// knowledgeClient builds a Knowledge pointed at the fake server, with retries
// that do not wait — the Knowledge counterpart of fakeDify.app in
// testutil_test.go, kept here because that file belongs to the app-side
// tests.
func knowledgeClient(t *testing.T, f *fakeDify, opts ...Option) *Knowledge {
	t.Helper()
	base := []Option{WithAPIKey("dataset-test-key-123456"), WithBaseURL(f.URL + "/v1")}
	k, err := NewKnowledge(append(base, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	wire(knowledgePort(k)).sleep = func(ctx context.Context, d time.Duration) error { return ctx.Err() }
	return k
}

// TestNewKnowledgeReadsTheDatasetKeyBeforeFallingBackToTheAppKey pins the
// fallback order in NewKnowledge's own doc comment: a dataset key is tried
// first, and only a program that never distinguishes the two credentials
// falls back to DIFY_API_KEY.
func TestNewKnowledgeReadsTheDatasetKeyBeforeFallingBackToTheAppKey(t *testing.T) {
	t.Setenv(EnvAPIKey, "app-fallback-key-000000")
	t.Setenv(EnvDatasetAPIKey, "")

	k, err := NewKnowledge()
	if err != nil {
		t.Fatal(err)
	}
	if got := wire(knowledgePort(k)).key.String(); got == "" {
		t.Fatal("expected a key resolved from DIFY_API_KEY")
	}

	t.Setenv(EnvDatasetAPIKey, "dataset-preferred-key-111111")
	k2, err := NewKnowledge()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := wire(knowledgePort(k2)).key.String(), MaskSecret("dataset-preferred-key-111111"); got != want {
		t.Fatalf("got %s, want the dataset key masked as %s", got, want)
	}
}

// TestKnowledgeModelsAsksTheDatasetScopedModelRoute pins the one Service-API
// route that takes the dataset token rather than an app key: getting this
// wrong reads like "Access token is invalid" rather than like the wrong
// client.
func TestKnowledgeModelsAsksTheDatasetScopedModelRoute(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"data": []any{
			map[string]any{
				"provider": "langgenius/openai/openai",
				"label":    map[string]any{"en_US": "OpenAI"},
				"status":   "active",
				"models": []any{
					map[string]any{"model": "text-embedding-3-small", "model_type": "text-embedding", "status": "active", "label": map[string]any{"en_US": "Embedding 3 Small"}},
				},
			},
		}})
	})
	k := knowledgeClient(t, f)
	providers, err := k.Models(context.Background(), "text-embedding")
	if err != nil {
		t.Fatal(err)
	}
	if got := f.last(t); got.Method != "GET" || got.Path != "/v1/workspaces/current/models/model-types/text-embedding" {
		t.Fatalf("got %s %s", got.Method, got.Path)
	}
	if len(providers) != 1 || providers[0].Label != "OpenAI" || len(providers[0].Models) != 1 {
		t.Fatalf("got %+v", providers)
	}
	m := providers[0].Models[0]
	if m.Name != "text-embedding-3-small" || m.Provider != "langgenius/openai/openai" || !m.Usable() {
		t.Fatalf("got %+v", m)
	}
}

// TestKnowledgeModelsDefaultsToLLM mirrors the Python SDK's default
// model_type="llm" when the caller leaves it out.
func TestKnowledgeModelsDefaultsToLLM(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"data": []any{}}) })
	k := knowledgeClient(t, f)
	if _, err := k.Models(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if got := f.last(t).Path; got != "/v1/workspaces/current/models/model-types/llm" {
		t.Fatalf("got %s", got)
	}
}

// TestLabelOfFallsBackToWhicheverLanguageIsThere pins that a provider or
// model labelled only in a language other than en_US still has a name,
// rather than reading as blank.
func TestLabelOfFallsBackToWhicheverLanguageIsThere(t *testing.T) {
	if got := labelOf(map[string]any{"zh_Hans": "你好"}); got != "你好" {
		t.Fatalf("got %q", got)
	}
	if got := labelOf("plain"); got != "plain" {
		t.Fatalf("got %q", got)
	}
	if got := labelOf(nil); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

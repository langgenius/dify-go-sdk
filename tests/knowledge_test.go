package tests

import (
	"context"
	"net/http"
	"testing"
	"time"

	dify "github.com/langgenius/dify-go-sdk"
	"github.com/langgenius/dify-go-sdk/internal/codec"
	"github.com/langgenius/dify-go-sdk/internal/usecase"
)

// knowledgeClient builds a Knowledge pointed at the fake server, with retries
// that do not wait — the Knowledge counterpart of fakeDify.app in
// testutil_test.go, kept here because that file belongs to the app-side
// tests.
func knowledgeClient(t *testing.T, f *fakeDify, opts ...dify.Option) *dify.Knowledge {
	t.Helper()
	base := []dify.Option{dify.WithAPIKey("dataset-test-key-123456"), dify.WithBaseURL(f.URL + "/v1")}
	k, err := dify.NewKnowledge(append(base, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	wire(usecase.KnowledgePort(k)).Sleep = func(ctx context.Context, d time.Duration) error { return ctx.Err() }
	return k
}

// TestNewKnowledgeReadsTheDatasetKeyBeforeFallingBackToTheAppKey pins the
// fallback order in NewKnowledge's own doc comment: a dataset key is tried
// first, and only a program that never distinguishes the two credentials
// falls back to DIFY_API_KEY.
func TestNewKnowledgeReadsTheDatasetKeyBeforeFallingBackToTheAppKey(t *testing.T) {
	t.Setenv(dify.EnvAPIKey, "app-fallback-key-000000")
	t.Setenv(dify.EnvDatasetAPIKey, "")

	k, err := dify.NewKnowledge()
	if err != nil {
		t.Fatal(err)
	}
	if got := wire(usecase.KnowledgePort(k)).Key.String(); got == "" {
		t.Fatal("expected a key resolved from DIFY_API_KEY")
	}

	t.Setenv(dify.EnvDatasetAPIKey, "dataset-preferred-key-111111")
	k2, err := dify.NewKnowledge()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := wire(usecase.KnowledgePort(k2)).Key.String(), dify.MaskSecret("dataset-preferred-key-111111"); got != want {
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
	if got := codec.LabelOf(map[string]any{"zh_Hans": "你好"}); got != "你好" {
		t.Fatalf("got %q", got)
	}
	if got := codec.LabelOf("plain"); got != "plain" {
		t.Fatalf("got %q", got)
	}
	if got := codec.LabelOf(nil); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

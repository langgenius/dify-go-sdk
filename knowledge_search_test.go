package dify

import (
	"errors"
	"testing"
)

// TestAScoreThresholdSetsBothFields pins the AGENTS.md warning: Dify ignores
// score_threshold unless score_threshold_enabled is true, so a threshold set
// alone would read as a filter that does nothing. RetrievalModel sets both
// from the one ScoreThreshold field.
func TestAScoreThresholdSetsBothFields(t *testing.T) {
	threshold := 0.75
	settings, err := RetrievalModel(&RetrievalModelParams{ScoreThreshold: &threshold})
	if err != nil {
		t.Fatal(err)
	}
	if settings["score_threshold_enabled"] != true {
		t.Errorf("score_threshold_enabled = %v, want true", settings["score_threshold_enabled"])
	}
	if settings["score_threshold"] != threshold {
		t.Errorf("score_threshold = %v, want %v", settings["score_threshold"], threshold)
	}
}

// TestLeavingScoreThresholdNilTurnsTheFlagOff pins the other half of that
// pair: no threshold means the flag is off, which is the only other state
// Dify has.
func TestLeavingScoreThresholdNilTurnsTheFlagOff(t *testing.T) {
	settings, err := RetrievalModel(nil)
	if err != nil {
		t.Fatal(err)
	}
	if settings["score_threshold_enabled"] != false || settings["score_threshold"] != nil {
		t.Errorf("got score_threshold_enabled=%v score_threshold=%v", settings["score_threshold_enabled"], settings["score_threshold"])
	}
}

// TestRerankAndWeightsTogetherAreRefused pins that Dify runs one reranking
// mode or the other — setting both is a contradiction, not a stronger
// rerank, and RetrievalModel refuses it before a request is ever sent.
func TestRerankAndWeightsTogetherAreRefused(t *testing.T) {
	weights := map[string]any{"vector_setting": map[string]any{}}
	_, err := RetrievalModel(&RetrievalModelParams{
		Rerank:  "langgenius/cohere/cohere:rerank-v3.5",
		Weights: weights,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("got %v, want ErrValidation", err)
	}
}

// TestWeightsLeaveReankingEnableOffOnBaseSettings pins that weighted score
// calls no model — Dify reads reranking_model only when reranking_enable is
// true, and on a knowledge base's own settings hybrid search reads the
// weights whatever that flag says. Setting Weights must not turn it on.
func TestWeightsLeaveReankingEnableOffOnBaseSettings(t *testing.T) {
	weights, err := WeightedScore(WeightedScoreParams{Embedding: "langgenius/openai/openai:text-embedding-3-small"})
	if err != nil {
		t.Fatal(err)
	}
	settings, err := RetrievalModel(&RetrievalModelParams{Search: SearchHybrid, Weights: weights})
	if err != nil {
		t.Fatal(err)
	}
	if settings["reranking_enable"] != false {
		t.Errorf("reranking_enable = %v, want false", settings["reranking_enable"])
	}
	if settings["reranking_mode"] != "weighted_score" {
		t.Errorf("reranking_mode = %v, want weighted_score", settings["reranking_mode"])
	}
	if settings["weights"] == nil {
		t.Error("weights not carried through")
	}
}

// TestRerankTurnsReankingEnableOnAndNamesTheModel pins the model-calling
// mode: reranking_enable true, and the provider/name split out of the
// colon-joined reference.
func TestRerankTurnsReankingEnableOnAndNamesTheModel(t *testing.T) {
	settings, err := RetrievalModel(&RetrievalModelParams{Rerank: "langgenius/cohere/cohere:rerank-v3.5"})
	if err != nil {
		t.Fatal(err)
	}
	if settings["reranking_enable"] != true {
		t.Errorf("reranking_enable = %v, want true", settings["reranking_enable"])
	}
	model, _ := settings["reranking_model"].(map[string]any)
	if model["reranking_provider_name"] != "langgenius/cohere/cohere" || model["reranking_model_name"] != "rerank-v3.5" {
		t.Errorf("got %v", model)
	}
}

// TestAnUnknownSearchMethodIsRefused pins that RetrievalModel validates
// Search against what Dify actually knows before sending it.
func TestAnUnknownSearchMethodIsRefused(t *testing.T) {
	_, err := RetrievalModel(&RetrievalModelParams{Search: "bm25"})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("got %v, want ErrValidation", err)
	}
}

// TestRetrievalModelDefaultsSearchAndTopK pins Dify's own defaults when a
// caller passes nil: semantic search, top_k 3.
func TestRetrievalModelDefaultsSearchAndTopK(t *testing.T) {
	settings, err := RetrievalModel(nil)
	if err != nil {
		t.Fatal(err)
	}
	if settings["search_method"] != SearchSemantic || settings["top_k"] != 3 {
		t.Fatalf("got %v", settings)
	}
}

// TestWeightedScoreSplitsTheEmbeddingReference pins that the vector setting
// names the provider and model the vector half was indexed with, split out
// of the one colon-joined reference.
func TestWeightedScoreSplitsTheEmbeddingReference(t *testing.T) {
	weights, err := WeightedScore(WeightedScoreParams{Embedding: "langgenius/openai/openai:text-embedding-3-small", Vector: 0.6, Keyword: 0.4})
	if err != nil {
		t.Fatal(err)
	}
	vs, _ := weights["vector_setting"].(map[string]any)
	if vs["embedding_provider_name"] != "langgenius/openai/openai" || vs["embedding_model_name"] != "text-embedding-3-small" || vs["vector_weight"] != 0.6 {
		t.Fatalf("got %v", vs)
	}
	ks, _ := weights["keyword_setting"].(map[string]any)
	if ks["keyword_weight"] != 0.4 {
		t.Fatalf("got %v", ks)
	}
}

// TestSplitModelRequiresBothAProviderAndAName pins the error a caller sees
// for a model reference missing its colon-joined name, naming the fix.
func TestSplitModelRequiresBothAProviderAndAName(t *testing.T) {
	for _, bad := range []string{"", "no-colon-here", ":missing-provider", "missing-name:"} {
		if _, _, err := splitModel(bad, "embedding"); !errors.Is(err, ErrValidation) {
			t.Errorf("reference=%q got %v, want ErrValidation", bad, err)
		}
	}
}

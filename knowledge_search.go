package dify

import "strings"

// How Dify may search a knowledge base.
const (
	SearchSemantic = "semantic_search"
	SearchFullText = "full_text_search"
	SearchHybrid   = "hybrid_search"
	SearchKeyword  = "keyword_search"
)

var searchMethods = map[string]bool{
	SearchSemantic: true,
	SearchFullText: true,
	SearchHybrid:   true,
	SearchKeyword:  true,
}

// splitModel splits Dify's "provider/plugin/vendor:model" into provider and
// name.
func splitModel(reference, what string) (provider, name string, err error) {
	i := strings.LastIndex(reference, ":")
	if i < 0 {
		return "", "", argError("%s=%q is missing a model name. Use 'provider/plugin/name:model', e.g. 'langgenius/openai/openai:text-embedding-3-small'.", what, reference)
	}
	provider, name = reference[:i], reference[i+1:]
	if provider == "" || name == "" {
		return "", "", argError("%s=%q is missing a model name. Use 'provider/plugin/name:model', e.g. 'langgenius/openai/openai:text-embedding-3-small'.", what, reference)
	}
	return provider, name, nil
}

// WeightedScoreParams builds the weights block for hybrid search.
type WeightedScoreParams struct {
	// Embedding is the model the vector half was indexed with — the blend is
	// computed against those embeddings, so naming a different model here
	// scores against vectors that do not exist.
	Embedding string
	// Vector and Keyword are the two weights. Left zero, both default to
	// Dify's own defaults (0.7 / 0.3) — which means there is no way to ask
	// for a weight of exactly zero here; pass RetrievalModel's Weights field
	// directly with a hand-built map if that is really what is wanted.
	Vector  float64
	Keyword float64
}

// WeightedScore blends vector and keyword scores instead of calling a rerank
// model:
//
//	weights, err := dify.WeightedScore(dify.WeightedScoreParams{Embedding: embeddingModel})
//	retrieval, err := dify.RetrievalModel(&dify.RetrievalModelParams{Search: dify.SearchHybrid, Weights: weights})
func WeightedScore(p WeightedScoreParams) (map[string]any, error) {
	provider, name, err := splitModel(p.Embedding, "embedding")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"vector_setting": map[string]any{
			"vector_weight":           firstNonZero(p.Vector, 0.7),
			"embedding_provider_name": provider,
			"embedding_model_name":    name,
		},
		"keyword_setting": map[string]any{"keyword_weight": firstNonZero(p.Keyword, 0.3)},
	}, nil
}

// RetrievalModelParams builds the retrieval_model block a knowledge base is
// searched by. How a knowledge base is searched is stored on the base, not
// passed per call — Datasets.Create's Retrieval field decides what every
// later retrieval does, a workflow's knowledge node included. Three things
// about that block are easy to get wrong, and all three are read out of
// Dify's retrieval code rather than its documentation:
//
//   - A score threshold is two fields. score_threshold is ignored unless
//     score_threshold_enabled is true, so a threshold set alone reads as a
//     filter that does nothing. Setting ScoreThreshold turns the flag on;
//     leaving it nil turns it off — those are the two states.
//   - An "economy" knowledge base is always searched by keyword, whatever
//     Search says: it has no embeddings to compare against. The setting is
//     kept because the base can be switched to "high_quality" later.
//   - Reranking has two modes and only one calls a model. Dify reads
//     reranking_model only when reranking_enable is true; Weights instead
//     blends the vector and keyword scores arithmetically, with no model
//     call. Setting both Rerank and Weights is not a stronger rerank, it is a
//     contradiction, and is refused here. On a knowledge base's own settings
//     (as opposed to a workflow's knowledge node over several bases),
//     hybrid search reads the weights whatever reranking_enable says, so
//     Weights leaves it off.
type RetrievalModelParams struct {
	// Search is one of SearchSemantic (default), SearchFullText, SearchHybrid
	// or SearchKeyword.
	Search string
	// TopK defaults to 3.
	TopK int
	// ScoreThreshold, when set, both supplies the threshold and turns on the
	// flag that makes Dify honour it.
	ScoreThreshold *float64
	// Rerank scores the merged results with a model, e.g.
	// "langgenius/cohere/cohere:rerank-v3.5". Mutually exclusive with
	// Weights.
	Rerank string
	// Weights blends the vector and keyword scores arithmetically, built with
	// WeightedScore. Mutually exclusive with Rerank.
	Weights map[string]any
}

// RetrievalModel builds the retrieval_model block. See RetrievalModelParams
// for what each setting does and the two reranking modes it refuses to
// combine.
func RetrievalModel(p *RetrievalModelParams) (map[string]any, error) {
	if p == nil {
		p = &RetrievalModelParams{}
	}
	search := firstNonZero(p.Search, SearchSemantic)
	if !searchMethods[search] {
		return nil, argError("search=%q is not one Dify knows. Use one of: %s, %s, %s, %s.", search, SearchSemantic, SearchFullText, SearchHybrid, SearchKeyword)
	}
	if p.Rerank != "" && p.Weights != nil {
		return nil, argError("Rerank scores with a model and Weights blends the scores arithmetically. Dify runs one or the other, so set one.")
	}

	settings := map[string]any{
		"search_method": search,
		"top_k":         firstNonZero(p.TopK, 3),
		// The flag is what makes the threshold count; the two are one setting
		// in Dify's UI and two fields on the wire.
		"score_threshold_enabled": p.ScoreThreshold != nil,
		"score_threshold":         nil,
		"reranking_enable":        false,
		"reranking_mode":          "reranking_model",
		"reranking_model":         nil,
		"weights":                 nil,
	}
	if p.ScoreThreshold != nil {
		settings["score_threshold"] = *p.ScoreThreshold
	}
	switch {
	case p.Rerank != "":
		provider, name, err := splitModel(p.Rerank, "rerank")
		if err != nil {
			return nil, err
		}
		settings["reranking_enable"] = true
		settings["reranking_model"] = map[string]any{
			"reranking_provider_name": provider,
			"reranking_model_name":    name,
		}
	case p.Weights != nil:
		// Weighted score calls no model, and Dify reads the rerank model only
		// when reranking_enable is set — so the mode is what selects it.
		settings["reranking_mode"] = "weighted_score"
		settings["weights"] = p.Weights
	}
	return settings, nil
}

// RetrievalHit is one segment retrieval found, and how well it matched.
type RetrievalHit struct {
	Score        float64
	Segment      *Segment
	DocumentID   string
	DocumentName string
}

func (h *RetrievalHit) String() string {
	if h.Segment != nil {
		return h.Segment.Content
	}
	return ""
}

// hitsFrom shapes a retrieval answer, from Datasets.Search or a pipeline's
// hit-testing.
func hitsFrom(o object) []*RetrievalHit {
	records := o.objs("records")
	if len(records) == 0 {
		records = o.obj("query").objs("records")
	}
	hits := make([]*RetrievalHit, 0, len(records))
	for _, r := range records {
		segment := r.obj("segment")
		document := segment.obj("document")
		hits = append(hits, &RetrievalHit{
			Score:        r.float("score"),
			Segment:      segmentFrom(segment),
			DocumentID:   document.str("id"),
			DocumentName: document.str("name"),
		})
	}
	return hits
}

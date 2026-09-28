package usecase

import (
	"context"
	"net/http"
	"strings"

	"github.com/langgenius/dify-go-sdk/internal/codec"
	"github.com/langgenius/dify-go-sdk/internal/entity"
	"github.com/langgenius/dify-go-sdk/internal/kernel"
	"github.com/langgenius/dify-go-sdk/internal/port"
)

// datasetPath builds a path rooted at one dataset.
func datasetPath(datasetID string, parts ...string) string {
	segments := append([]string{"datasets", port.PathEscape(datasetID)}, parts...)
	return "/" + strings.Join(segments, "/")
}

// Datasets are the workspace's knowledge bases.
type Datasets struct{ api port.Port }

// DatasetCreateParams are the optional parts of creating a knowledge base.
type DatasetCreateParams struct {
	Description string
	// IndexingTechnique is "high_quality" or "economy". "economy" searches by
	// keyword and uses no model; "high_quality" is indexed with Embedding.
	IndexingTechnique string
	Permission        string
	// Embedding is the model chunks are indexed with, spelled the way every
	// other model reference is — "provider/plugin/name:model", from
	// Knowledge.Models("text-embedding"). Only meaningful for "high_quality"
	// indexing.
	Embedding string
	// Retrieval is how the base is searched, built with RetrievalModel. This
	// is a setting on the base, so it decides how every later retrieval
	// behaves — including the one a workflow's knowledge node does.
	Retrieval map[string]any
	// Provider is "vendor" (default, an internal knowledge base) or
	// "external".
	Provider               string
	ExternalKnowledgeAPIID string
	ExternalKnowledgeID    string
	SummaryIndexSetting    map[string]any
}

// Create creates a knowledge base.
func (d *Datasets) Create(ctx context.Context, name string, p *DatasetCreateParams) (*entity.Dataset, error) {
	if p == nil {
		p = &DatasetCreateParams{}
	}
	body := map[string]any{"name": name}
	if p.Description != "" {
		body["description"] = p.Description
	}
	if p.IndexingTechnique != "" {
		body["indexing_technique"] = p.IndexingTechnique
	}
	if p.Permission != "" {
		body["permission"] = p.Permission
	}
	if p.Provider != "" {
		body["provider"] = p.Provider
	}
	if p.ExternalKnowledgeAPIID != "" {
		body["external_knowledge_api_id"] = p.ExternalKnowledgeAPIID
	}
	if p.ExternalKnowledgeID != "" {
		body["external_knowledge_id"] = p.ExternalKnowledgeID
	}
	if p.Retrieval != nil {
		body["retrieval_model"] = p.Retrieval
	}
	if p.SummaryIndexSetting != nil {
		body["summary_index_setting"] = p.SummaryIndexSetting
	}
	if p.Embedding != "" {
		provider, model, err := SplitModel(p.Embedding, "embedding")
		if err != nil {
			return nil, err
		}
		body["embedding_model_provider"] = provider
		body["embedding_model"] = model
	}
	o, err := d.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/datasets", Body: body})
	if err != nil {
		return nil, err
	}
	return codec.DatasetFrom(o), nil
}

// DatasetListParams narrow the workspace's knowledge base listing.
type DatasetListParams struct {
	Page       int
	Limit      int
	Keyword    string
	TagIDs     []string
	IncludeAll bool
}

// List lists the workspace's knowledge bases.
func (d *Datasets) List(ctx context.Context, p *DatasetListParams) (*codec.Page[*entity.Dataset], error) {
	if p == nil {
		p = &DatasetListParams{}
	}
	fetch := func(ctx context.Context, number int) (kernel.Object, error) {
		q := port.Params{}.SetInt("page", number).SetInt("limit", p.Limit).Set("keyword", p.Keyword).SetBool("include_all", p.IncludeAll)
		values := q.Values()
		// Dify reads this as request.args.getlist("tag_ids"): repeated
		// parameters, not one comma-joined value.
		for _, id := range p.TagIDs {
			values.Add("tag_ids", id)
		}
		return d.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/datasets", Query: values})
	}
	return codec.FetchByPage(ctx, codec.DatasetFrom, fetch, p.Page)
}

// Retrieve reads one knowledge base back.
func (d *Datasets) Retrieve(ctx context.Context, datasetID string) (*entity.Dataset, error) {
	o, err := d.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: datasetPath(datasetID)})
	if err != nil {
		return nil, err
	}
	return codec.DatasetFrom(o), nil
}

// DatasetUpdateParams are the changeable settings of a knowledge base. A nil
// field is left as it was; only what is set here is sent, since Dify updates
// only the fields present in the PATCH body.
type DatasetUpdateParams struct {
	Name              *string
	Description       *string
	IndexingTechnique *string
	Permission        *string
	// Embedding replaces both embedding_model and embedding_model_provider.
	Embedding *string
	Retrieval map[string]any
	// PartialMemberList names who may access the base when Permission is
	// "partial_members".
	PartialMemberList      []string
	ExternalRetrievalModel map[string]any
	ExternalKnowledgeID    *string
	ExternalKnowledgeAPIID *string
}

// Update changes a knowledge base's settings.
func (d *Datasets) Update(ctx context.Context, datasetID string, p *DatasetUpdateParams) (*entity.Dataset, error) {
	if p == nil {
		p = &DatasetUpdateParams{}
	}
	body := map[string]any{}
	setStr := func(key string, v *string) {
		if v != nil {
			body[key] = *v
		}
	}
	setStr("name", p.Name)
	setStr("description", p.Description)
	setStr("indexing_technique", p.IndexingTechnique)
	setStr("permission", p.Permission)
	setStr("external_knowledge_id", p.ExternalKnowledgeID)
	setStr("external_knowledge_api_id", p.ExternalKnowledgeAPIID)
	if p.Retrieval != nil {
		body["retrieval_model"] = p.Retrieval
	}
	if p.PartialMemberList != nil {
		body["partial_member_list"] = p.PartialMemberList
	}
	if p.ExternalRetrievalModel != nil {
		body["external_retrieval_model"] = p.ExternalRetrievalModel
	}
	if p.Embedding != nil {
		provider, model, err := SplitModel(*p.Embedding, "embedding")
		if err != nil {
			return nil, err
		}
		body["embedding_model_provider"] = provider
		body["embedding_model"] = model
	}
	o, err := d.api.Call(ctx, &port.Request{Method: http.MethodPatch, Path: datasetPath(datasetID), Body: body})
	if err != nil {
		return nil, err
	}
	return codec.DatasetFrom(o), nil
}

// Delete deletes a knowledge base and everything in it — its documents, and
// its pipeline if it has one: there is no separate route that deletes a
// pipeline.
func (d *Datasets) Delete(ctx context.Context, datasetID string) error {
	_, err := d.api.Call(ctx, &port.Request{Method: http.MethodDelete, Path: datasetPath(datasetID)})
	return err
}

// SearchParams are the optional parts of a retrieval call.
type SearchParams struct {
	// RetrievalModel overrides the knowledge base's own retrieval settings
	// for this call, built with RetrievalModel.
	RetrievalModel map[string]any
	// ExternalRetrievalModel is retrieval settings for an external knowledge
	// base: top_k, score_threshold and score_threshold_enabled.
	ExternalRetrievalModel map[string]any
}

// Search retrieves against a knowledge base, as a node would — the same
// thing the console calls hit testing: it runs retrieval and shows what came
// back, without an app in the way. Dify serves this at /retrieve;
// /hit-testing is the deprecated alias of the same route.
func (d *Datasets) Search(ctx context.Context, datasetID, query string, p *SearchParams) ([]*entity.RetrievalHit, error) {
	body := map[string]any{"query": query}
	if p != nil {
		if p.RetrievalModel != nil {
			body["retrieval_model"] = p.RetrievalModel
		}
		if p.ExternalRetrievalModel != nil {
			body["external_retrieval_model"] = p.ExternalRetrievalModel
		}
	}
	o, err := d.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: datasetPath(datasetID, "retrieve"), Body: body})
	if err != nil {
		return nil, err
	}
	return codec.HitsFrom(o), nil
}

// Tags is the tags bound to one knowledge base. The other direction —
// Tags.Bind and Tags.Unbind — lives on the workspace-level Tags resource.
func (d *Datasets) Tags(ctx context.Context, datasetID string) ([]*entity.Tag, error) {
	o, err := d.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: datasetPath(datasetID, "tags")})
	if err != nil {
		return nil, err
	}
	return codec.TagsFrom(o), nil
}

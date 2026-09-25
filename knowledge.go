package dify

import (
	"context"
	"fmt"
	"net/http"
)

// Knowledge is the workspace's knowledge bases, addressed by a dataset API
// key.
//
// A dataset key is not an app key: it scopes to the workspace's datasets and
// can do nothing to an app, which is why this is a separate client from App
// rather than another field on it.
//
//	knowledge, err := dify.NewKnowledge(dify.WithAPIKey("dataset-…"))
//	dataset, err := knowledge.Datasets.Create(ctx, "handbook", nil)
//	docs := knowledge.Documents(dataset.ID)
//	doc, err := docs.CreateFromText(ctx, "policy", "…", nil)
//	docs.WaitUntilIndexed(ctx, doc.Batch, nil)
//	hits, err := knowledge.Datasets.Search(ctx, dataset.ID, "what is the refund window?", nil)
//
// A Knowledge is safe for concurrent use.
type Knowledge struct {
	t *transport

	// Datasets are the workspace's knowledge bases.
	Datasets *Datasets
	// Tags are workspace-level rather than per-base: a tag exists once and is
	// bound to as many knowledge bases as you like.
	Tags *Tags
}

// NewKnowledge builds a client for the workspace's knowledge bases. The key
// comes from WithAPIKey, then DIFY_DATASET_API_KEY, then DIFY_API_KEY — the
// fallback is where a single-purpose program keeps a dataset key it never
// distinguishes from an app key. It sends nothing.
func NewKnowledge(opts ...Option) (*Knowledge, error) {
	t, err := newTransport(opts, EnvDatasetAPIKey, EnvAPIKey)
	if err != nil {
		return nil, err
	}
	return &Knowledge{t: t, Datasets: &Datasets{t}, Tags: &Tags{t}}, nil
}

// BaseURL is the Service API root this client sends to.
func (k *Knowledge) BaseURL() string { return k.t.baseURL }

func (k *Knowledge) String() string {
	return fmt.Sprintf("dify.Knowledge(base_url=%q, api_key=%s)", k.t.baseURL, k.t.key)
}

// GoString keeps %#v from printing the key.
func (k *Knowledge) GoString() string { return k.String() }

// Documents is the documents of one knowledge base. Bound to a dataset id so
// it is not repeated on every call.
func (k *Knowledge) Documents(datasetID string) *Documents {
	return &Documents{t: k.t, datasetID: datasetID}
}

// Pipeline is one knowledge base's RAG pipeline: how documents get in and get
// indexed. Not every knowledge base has one — see the type's own doc comment.
func (k *Knowledge) Pipeline(datasetID string) *Pipeline {
	return &Pipeline{t: k.t, datasetID: datasetID}
}

// Models is the models of one type the workspace has configured — mostly
// asked for "text-embedding" and "rerank", which is what configuring a
// knowledge base needs. modelType empty means "llm".
//
// On Knowledge rather than App because Dify guards this route with the
// *dataset* token: an app key answers "Access token is invalid", which reads
// like a broken key rather than like the wrong client.
func (k *Knowledge) Models(ctx context.Context, modelType string) ([]*ModelProvider, error) {
	o, err := k.t.call(ctx, &request{method: http.MethodGet, path: "/workspaces/current/models/model-types/" + pathEscape(firstNonZero(modelType, "llm"))})
	if err != nil {
		return nil, err
	}
	return providersFrom(o.list("data")), nil
}

// UploadForPipeline uploads a file for a pipeline to consume. Workspace-level
// rather than per-pipeline, which is why it hangs off Knowledge and not
// Pipeline.
func (k *Knowledge) UploadForPipeline(ctx context.Context, file Upload) (map[string]any, error) {
	part, err := pipelinePart(file)
	if err != nil {
		return nil, err
	}
	o, err := k.t.call(ctx, &request{method: http.MethodPost, path: "/datasets/pipeline/file-upload", form: &multipartForm{file: part}})
	if err != nil {
		return nil, err
	}
	return o.raw(), nil
}

// Model is one model this workspace can call.
type Model struct {
	// Name is what to pass as "model". Dify spells this "model" in the
	// payload; it is the identifier, so it is Name here and Label is the
	// human-readable one.
	Name string
	// Provider is not in the model's own payload — carried down from the
	// provider it was listed under, because every call that takes a model
	// name takes a provider beside it.
	Provider   string
	Label      string
	Type       string
	Status     string
	Deprecated bool
	// Properties is context size, max chunks and the like. Provider-specific,
	// so a map.
	Properties map[string]any
	Raw        map[string]any
}

// Usable reports whether Dify says this model can be called right now.
func (m *Model) Usable() bool { return m.Status == "active" && !m.Deprecated }

func (m *Model) String() string { return m.Name }

// ModelProvider is a configured provider, and the models it offers.
type ModelProvider struct {
	Provider string
	Label    string
	Status   string
	Models   []*Model
	Raw      map[string]any
}

func (p *ModelProvider) String() string { return p.Provider }

// labelOf reads one readable label out of Dify's {"en_US": …, "zh_Hans": …},
// falling back to whatever language is there rather than to an empty string:
// a provider labelled only in Chinese still has a name.
func labelOf(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case map[string]any:
		if s, ok := v["en_US"].(string); ok && s != "" {
			return s
		}
		for _, other := range v {
			if s, ok := other.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func modelFrom(o object, provider string) *Model {
	return &Model{
		Name:       o.str("model"),
		Provider:   provider,
		Label:      labelOf(o["label"]),
		Type:       o.str("model_type"),
		Status:     o.str("status"),
		Deprecated: o.bool("deprecated"),
		Properties: o.obj("model_properties").raw(),
		Raw:        o.raw(),
	}
}

// providersFrom shapes a model listing, from either the Service API or the
// console.
func providersFrom(items []any) []*ModelProvider {
	out := make([]*ModelProvider, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		o := object(m)
		provider := o.str("provider")
		var models []*Model
		for _, mp := range o.objs("models") {
			models = append(models, modelFrom(mp, provider))
		}
		out = append(out, &ModelProvider{
			Provider: provider,
			Label:    labelOf(o["label"]),
			Status:   o.str("status"),
			Models:   models,
			Raw:      o.raw(),
		})
	}
	return out
}

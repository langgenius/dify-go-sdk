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
	api port

	// Datasets are the workspace's knowledge bases.
	Datasets *Datasets
	// Tags are workspace-level rather than per-base: a tag exists once and is
	// bound to as many knowledge bases as you like.
	Tags *Tags
}

// BaseURL is the Service API root this client sends to.
func (k *Knowledge) BaseURL() string { return k.api.endpoint("") }

func (k *Knowledge) String() string {
	return fmt.Sprintf("dify.Knowledge(base_url=%q, api_key=%s)", k.api.endpoint(""), k.api.maskedKey())
}

// GoString keeps %#v from printing the key.
func (k *Knowledge) GoString() string { return k.String() }

// Documents is the documents of one knowledge base. Bound to a dataset id so
// it is not repeated on every call.
func (k *Knowledge) Documents(datasetID string) *Documents {
	return &Documents{api: k.api, datasetID: datasetID}
}

// Pipeline is one knowledge base's RAG pipeline: how documents get in and get
// indexed. Not every knowledge base has one — see the type's own doc comment.
func (k *Knowledge) Pipeline(datasetID string) *Pipeline {
	return &Pipeline{api: k.api, datasetID: datasetID}
}

// Models is the models of one type the workspace has configured — mostly
// asked for "text-embedding" and "rerank", which is what configuring a
// knowledge base needs. modelType empty means "llm".
//
// On Knowledge rather than App because Dify guards this route with the
// *dataset* token: an app key answers "Access token is invalid", which reads
// like a broken key rather than like the wrong client.
func (k *Knowledge) Models(ctx context.Context, modelType string) ([]*ModelProvider, error) {
	o, err := k.api.call(ctx, &request{method: http.MethodGet, path: "/workspaces/current/models/model-types/" + pathEscape(firstNonZero(modelType, "llm"))})
	if err != nil {
		return nil, err
	}
	return modelProvidersFrom(o), nil
}

// UploadForPipeline uploads a file for a pipeline to consume. Workspace-level
// rather than per-pipeline, which is why it hangs off Knowledge and not
// Pipeline.
func (k *Knowledge) UploadForPipeline(ctx context.Context, file Upload) (map[string]any, error) {
	part, err := pipelinePart(file)
	if err != nil {
		return nil, err
	}
	o, err := k.api.call(ctx, &request{method: http.MethodPost, path: "/datasets/pipeline/file-upload", form: &multipartForm{file: part}})
	if err != nil {
		return nil, err
	}
	return o.raw(), nil
}

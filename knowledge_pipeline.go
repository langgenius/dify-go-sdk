package dify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
)

// Pipeline is a knowledge base's RAG pipeline: how documents get in and get
// indexed.
//
// A pipeline is a workflow in its own right — datasource nodes that fetch,
// and processing that chunks and embeds. These methods run it, rather than
// letting Dify run it on upload.
//
// Not every knowledge base has one. A base created with Datasets.Create
// indexes on upload and has no pipeline; Dify answers every call here on an
// ordinary knowledge base with "Pipeline not found", which reads like a bug
// rather than like an absence — this rewords it. A pipeline comes from
// creating the base from a pipeline template in the console; there is no
// route that deletes a pipeline on its own, either — Datasets.Delete removes
// both, and a pipeline has no listing of its own, only the dataset rows that
// carry one.
type Pipeline struct {
	api       port
	datasetID string
}

// call sends, turning "Pipeline not found" into something actionable.
func (p *Pipeline) call(ctx context.Context, r *request) (object, error) {
	o, err := p.api.call(ctx, r)
	if err != nil {
		return nil, wrapNoPipeline(p.datasetID, err)
	}
	return o, nil
}

func wrapNoPipeline(datasetID string, err error) error {
	var apiErr *APIError
	if errors.As(err, &apiErr) && strings.Contains(strings.ToLower(apiErr.Message), "pipeline not found") {
		return fmt.Errorf("dify: knowledge base %s has no RAG pipeline. One created with Datasets.Create indexes on upload instead; a pipeline comes from creating the base from a pipeline template in the console: %w", datasetID, err)
	}
	return err
}

// DatasourcesParams controls whether Datasources reads the published or
// draft pipeline.
type DatasourcesParams struct {
	// Published defaults to true — the published pipeline's datasource
	// nodes, matching Dify's own default.
	Published *bool
}

func publishedOr(p *bool) bool {
	if p == nil {
		return true
	}
	return *p
}

// Datasources lists the datasource plugins this pipeline can pull from.
func (pl *Pipeline) Datasources(ctx context.Context, p *DatasourcesParams) ([]map[string]any, error) {
	if p == nil {
		p = &DatasourcesParams{}
	}
	o, err := pl.call(ctx, &request{
		method: http.MethodGet,
		path:   datasetPath(pl.datasetID, "pipeline", "datasource-plugins"),
		query:  params{}.setBool("is_published", publishedOr(p.Published)).values(),
	})
	if err != nil {
		return nil, err
	}
	return dataMaps(o), nil
}

// RunDatasourceParams are the optional parts of running one datasource node.
type RunDatasourceParams struct {
	CredentialID string
	// Published defaults to true, as in DatasourcesParams.
	Published *bool
}

// RunDatasourceNode runs one datasource node — fetch, without indexing what
// it found.
func (pl *Pipeline) RunDatasourceNode(ctx context.Context, nodeID, datasourceType string, inputs map[string]any, p *RunDatasourceParams) (map[string]any, error) {
	if p == nil {
		p = &RunDatasourceParams{}
	}
	body := map[string]any{
		"inputs":          orEmpty(inputs),
		"datasource_type": datasourceType,
		"is_published":    publishedOr(p.Published),
	}
	if p.CredentialID != "" {
		body["credential_id"] = p.CredentialID
	}
	o, err := pl.call(ctx, &request{method: http.MethodPost, path: datasetPath(pl.datasetID, "pipeline", "datasource", "nodes", pathEscape(nodeID), "run"), body: body})
	if err != nil {
		return nil, err
	}
	return o.raw(), nil
}

// PipelineRunInput is what a pipeline run needs, published or draft. Every
// field but Inputs is required, which is why this is a plain struct rather
// than a *Params one — there is no sane default for "which datasource, from
// where".
type PipelineRunInput struct {
	// StartNodeID is the datasource node the run starts at.
	StartNodeID string
	// DatasourceType determines which fields DatasourceInfoList items need:
	// "upload_file", "notion_import" or "website_crawl".
	DatasourceType string
	// DatasourceInfoList is one object per source to process.
	DatasourceInfoList []map[string]any
	// Inputs are the pipeline's own input variables. Pass nil or an empty map
	// when the pipeline declares none.
	Inputs map[string]any
}

func (in PipelineRunInput) body(published bool, responseMode string) map[string]any {
	return map[string]any{
		"start_node_id":        in.StartNodeID,
		"datasource_type":      in.DatasourceType,
		"datasource_info_list": in.DatasourceInfoList,
		"inputs":               orEmpty(in.Inputs),
		"is_published":         published,
		"response_mode":        responseMode,
	}
}

// Run runs the published pipeline over the given sources.
//
// Queued, not awaited: Dify enqueues one document per source and answers
// straight away with the batch they share. Nothing is indexed yet, so there
// is no run to watch and no stream to read — wait on the documents instead:
//
//	queued, err := knowledge.Pipeline(datasetID).Run(ctx, in)
//	status, err := knowledge.Documents(datasetID).WaitUntilIndexed(ctx, queued.Batch, nil)
//
// RunDraft is the other thing this route does: running the *unpublished*
// graph, which is a workflow run and reports like one.
func (pl *Pipeline) Run(ctx context.Context, in PipelineRunInput) (*PipelineIngestion, error) {
	o, err := pl.call(ctx, &request{method: http.MethodPost, path: datasetPath(pl.datasetID, "pipeline", "run"), body: in.body(true, "blocking")})
	if err != nil {
		return nil, err
	}
	return ingestionFrom(o, pl.datasetID), nil
}

// RunDraft runs the *draft* pipeline and waits for it, as the console does.
//
// A draft run indexes nothing and queues nothing: it executes the graph so
// you can see what it would do. Dify runs it as a workflow and reports it as
// one, which is why this returns the same *WorkflowRun as
// App.Workflows.Runs rather than a type of its own.
func (pl *Pipeline) RunDraft(ctx context.Context, in PipelineRunInput) (*WorkflowRun, error) {
	o, err := pl.call(ctx, &request{method: http.MethodPost, path: datasetPath(pl.datasetID, "pipeline", "run"), body: in.body(false, "blocking")})
	if err != nil {
		return nil, err
	}
	return runFromBlocking(o), nil
}

// StreamDraft runs the draft pipeline and watches it happen.
//
// The events are workflow events — Dify sends the pipeline's run through the
// same stream — so this is a *WorkflowRunStream, iterated and closed like
// any other. Streaming a *published* run is not a thing: that one is queued,
// and Run returns as soon as it is.
func (pl *Pipeline) StreamDraft(ctx context.Context, in PipelineRunInput) (*WorkflowRunStream, error) {
	events, err := pl.api.stream(ctx, &request{method: http.MethodPost, path: datasetPath(pl.datasetID, "pipeline", "run"), body: in.body(false, "streaming")})
	if err != nil {
		return nil, wrapNoPipeline(pl.datasetID, err)
	}
	return newWorkflowRunStream(events, true), nil
}

// pipelinePart is the part a pipeline upload sends, with the extension Dify
// reads.
//
// Dify picks the reader by extension and refuses a name without one —
// "Unsupported Extension Type: ." — and the document then fails indexing
// rather than the upload. A fallback name with no extension would do exactly
// that to every upload made without one set explicitly.
func pipelinePart(file Upload) (*filePart, error) {
	part, err := file.part("file", false)
	if err != nil {
		return nil, err
	}
	if filepath.Ext(part.name) == "" {
		return nil, argError("%q has no file extension, and Dify chooses how to read a document by its extension. Set Upload.Name to include one, e.g. %q.", part.name, part.name+".pdf")
	}
	return part, nil
}

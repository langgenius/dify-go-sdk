package dify

import (
	"context"
	"net/http"
)

// Annotations are this app's annotations, and the reply setting that uses
// them.
type Annotations struct{ api port }

// AnnotationListParams narrow the annotations.
type AnnotationListParams struct {
	Page    int
	Limit   int
	Keyword string
}

// List is the app's annotations.
func (a *Annotations) List(ctx context.Context, p *AnnotationListParams) (*Page[Annotation], error) {
	if p == nil {
		p = &AnnotationListParams{}
	}
	fetch := func(ctx context.Context, number int) (object, error) {
		q := params{}.setInt("page", number).setInt("limit", p.Limit).set("keyword", p.Keyword)
		return a.api.call(ctx, &request{method: http.MethodGet, path: "/apps/annotations", query: q.values()})
	}
	return fetchByPage(ctx, annotationFrom, fetch, p.Page)
}

// Create adds an annotation.
func (a *Annotations) Create(ctx context.Context, question, answer string) (*Annotation, error) {
	o, err := a.api.call(ctx, &request{method: http.MethodPost, path: "/apps/annotations", body: map[string]any{"question": question, "answer": answer}})
	if err != nil {
		return nil, err
	}
	an := annotationFrom(o)
	return &an, nil
}

// Update replaces an annotation's question and answer.
func (a *Annotations) Update(ctx context.Context, annotationID, question, answer string) (*Annotation, error) {
	o, err := a.api.call(ctx, &request{method: http.MethodPut, path: "/apps/annotations/" + pathEscape(annotationID), body: map[string]any{"question": question, "answer": answer}})
	if err != nil {
		return nil, err
	}
	an := annotationFrom(o)
	return &an, nil
}

// Delete removes an annotation.
func (a *Annotations) Delete(ctx context.Context, annotationID string) error {
	_, err := a.api.call(ctx, &request{method: http.MethodDelete, path: "/apps/annotations/" + pathEscape(annotationID)})
	return err
}

// ReplySettings configure annotation reply. Dify's payload model requires all
// three fields on enable and on disable alike, so a disable has to name them
// too; leaving them out answers 422.
type ReplySettings struct {
	EmbeddingProvider string
	EmbeddingModel    string
	// ScoreThreshold is the similarity an annotation must reach to be used.
	ScoreThreshold float64
}

// SetReply turns annotation reply on or off. Enabling it re-embeds every
// annotation, so Dify answers with a job; poll it with ReplyStatus.
func (a *Annotations) SetReply(ctx context.Context, enabled bool, s ReplySettings) (*AnnotationReplyJob, error) {
	if s.EmbeddingProvider == "" || s.EmbeddingModel == "" {
		return nil, argError("annotation reply needs EmbeddingProvider and EmbeddingModel, even to disable it: Dify validates the same payload for both")
	}
	body := map[string]any{
		"embedding_provider_name": s.EmbeddingProvider,
		"embedding_model_name":    s.EmbeddingModel,
		"score_threshold":         s.ScoreThreshold,
	}
	o, err := a.api.call(ctx, &request{method: http.MethodPost, path: "/apps/annotation-reply/" + replyAction(enabled), body: body})
	if err != nil {
		return nil, err
	}
	return jobFrom(o, ""), nil
}

// ReplyStatus asks how an enable or disable job is going. enabled says which
// of the two the job was started by.
func (a *Annotations) ReplyStatus(ctx context.Context, jobID string, enabled bool) (*AnnotationReplyJob, error) {
	o, err := a.api.call(ctx, &request{method: http.MethodGet, path: "/apps/annotation-reply/" + replyAction(enabled) + "/status/" + pathEscape(jobID)})
	if err != nil {
		return nil, err
	}
	return jobFrom(o, jobID), nil
}

func replyAction(enabled bool) string {
	if enabled {
		return "enable"
	}
	return "disable"
}

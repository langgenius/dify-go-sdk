package usecase

import (
	"context"
	"net/http"

	"github.com/langgenius/dify-go-sdk/internal/codec"
	"github.com/langgenius/dify-go-sdk/internal/entity"
	"github.com/langgenius/dify-go-sdk/internal/kernel"
	"github.com/langgenius/dify-go-sdk/internal/port"
)

// Annotations are this app's annotations, and the reply setting that uses
// them.
type Annotations struct{ api port.Port }

// AnnotationListParams narrow the annotations.
type AnnotationListParams struct {
	Page    int
	Limit   int
	Keyword string
}

// List is the app's annotations.
func (a *Annotations) List(ctx context.Context, p *AnnotationListParams) (*codec.Page[entity.Annotation], error) {
	if p == nil {
		p = &AnnotationListParams{}
	}
	fetch := func(ctx context.Context, number int) (kernel.Object, error) {
		q := port.Params{}.SetInt("page", number).SetInt("limit", p.Limit).Set("keyword", p.Keyword)
		return a.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/apps/annotations", Query: q.Values()})
	}
	return codec.FetchByPage(ctx, codec.AnnotationFrom, fetch, p.Page)
}

// Create adds an annotation.
func (a *Annotations) Create(ctx context.Context, question, answer string) (*entity.Annotation, error) {
	o, err := a.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/apps/annotations", Body: map[string]any{"question": question, "answer": answer}})
	if err != nil {
		return nil, err
	}
	an := codec.AnnotationFrom(o)
	return &an, nil
}

// Update replaces an annotation's question and answer.
func (a *Annotations) Update(ctx context.Context, annotationID, question, answer string) (*entity.Annotation, error) {
	o, err := a.api.Call(ctx, &port.Request{Method: http.MethodPut, Path: "/apps/annotations/" + port.PathEscape(annotationID), Body: map[string]any{"question": question, "answer": answer}})
	if err != nil {
		return nil, err
	}
	an := codec.AnnotationFrom(o)
	return &an, nil
}

// Delete removes an annotation.
func (a *Annotations) Delete(ctx context.Context, annotationID string) error {
	_, err := a.api.Call(ctx, &port.Request{Method: http.MethodDelete, Path: "/apps/annotations/" + port.PathEscape(annotationID)})
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
func (a *Annotations) SetReply(ctx context.Context, enabled bool, s ReplySettings) (*entity.AnnotationReplyJob, error) {
	if s.EmbeddingProvider == "" || s.EmbeddingModel == "" {
		return nil, kernel.ArgError("annotation reply needs EmbeddingProvider and EmbeddingModel, even to disable it: Dify validates the same payload for both")
	}
	body := map[string]any{
		"embedding_provider_name": s.EmbeddingProvider,
		"embedding_model_name":    s.EmbeddingModel,
		"score_threshold":         s.ScoreThreshold,
	}
	o, err := a.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/apps/annotation-reply/" + replyAction(enabled), Body: body})
	if err != nil {
		return nil, err
	}
	return codec.JobFrom(o, ""), nil
}

// ReplyStatus asks how an enable or disable job is going. enabled says which
// of the two the job was started by.
func (a *Annotations) ReplyStatus(ctx context.Context, jobID string, enabled bool) (*entity.AnnotationReplyJob, error) {
	o, err := a.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/apps/annotation-reply/" + replyAction(enabled) + "/status/" + port.PathEscape(jobID)})
	if err != nil {
		return nil, err
	}
	return codec.JobFrom(o, jobID), nil
}

func replyAction(enabled bool) string {
	if enabled {
		return "enable"
	}
	return "disable"
}

package codec

import (
	"github.com/langgenius/dify-go-sdk/internal/entity"
	"github.com/langgenius/dify-go-sdk/internal/kernel"
)

func DatasetFrom(o kernel.Object) *entity.Dataset {
	return &entity.Dataset{
		ID:                o.Str("id"),
		Name:              o.Str("name"),
		Description:       o.Str("description"),
		Permission:        o.Str("permission"),
		IndexingTechnique: o.Str("indexing_technique"),
		DocumentCount:     o.Int("document_count"),
		WordCount:         o.Int("word_count"),
		AppCount:          o.Int("app_count"),
		Raw:               o.Raw(),
	}
}

// boolOr reads a boolean that defaults to true when absent — Dify's own
// "enabled" field on a document or segment, which is true unless explicitly
// turned off.
func boolOr(o kernel.Object, key string, def bool) bool {
	if !o.Has(key) {
		return def
	}
	return o.Bool(key)
}

func DocumentFrom(o kernel.Object) *entity.Document {
	return &entity.Document{
		ID:             o.Str("id"),
		Name:           o.Str("name"),
		IndexingStatus: o.Str("indexing_status"),
		WordCount:      o.Int("word_count"),
		Enabled:        boolOr(o, "enabled", true),
		Error:          o.Str("error"),
		Batch:          o.Str("batch"),
		Raw:            o.Raw(),
	}
}

// CreatedDocumentFrom shapes a document as Create and Update report it. Dify
// nests the document under "document" and puts the indexing batch beside it,
// not inside it — so the batch has to be carried across, or the document
// cannot be asked about its own indexing.
func CreatedDocumentFrom(o kernel.Object) *entity.Document {
	doc := o.Obj("document")
	if len(doc) == 0 {
		doc = o
	}
	batch := kernel.FirstNonZero(o.Str("batch"), doc.Str("batch"))
	merged := make(kernel.Object, len(doc)+1)
	for k, v := range doc {
		merged[k] = v
	}
	merged["batch"] = batch
	return DocumentFrom(merged)
}

// StatusFrom shapes an indexing-status answer, which arrives as a
// one-item array.
func StatusFrom(o kernel.Object) *entity.IndexingStatus {
	items := o.Objs("data")
	data := kernel.Object{}
	if len(items) > 0 {
		data = items[0]
	}
	return &entity.IndexingStatus{
		ID:                data.Str("id"),
		Status:            data.Str("indexing_status"),
		CompletedSegments: data.Int("completed_segments"),
		TotalSegments:     data.Int("total_segments"),
		Error:             data.Str("error"),
	}
}

func MetadataFieldFrom(o kernel.Object) *entity.MetadataField {
	return &entity.MetadataField{
		Name:  o.Str("name"),
		Type:  o.Str("type"),
		ID:    o.Str("id"),
		Count: o.Int("count"),
		Raw:   o.Raw(),
	}
}

func IngestionFrom(o kernel.Object, datasetID string) *entity.PipelineIngestion {
	batch := o.Str("batch")
	documents := o.Objs("documents")
	docs := make([]*entity.Document, 0, len(documents))
	for _, item := range documents {
		merged := make(kernel.Object, len(item)+1)
		for k, v := range item {
			merged[k] = v
		}
		merged["batch"] = batch
		docs = append(docs, DocumentFrom(merged))
	}
	id := o.Obj("dataset").Str("id")
	if id == "" {
		id = datasetID
	}
	return &entity.PipelineIngestion{Batch: batch, Documents: docs, DatasetID: id, Raw: o.Raw()}
}

// HitsFrom shapes a retrieval answer, from Datasets.Search or a pipeline's
// hit-testing.
func HitsFrom(o kernel.Object) []*entity.RetrievalHit {
	records := o.Objs("records")
	if len(records) == 0 {
		records = o.Obj("query").Objs("records")
	}
	hits := make([]*entity.RetrievalHit, 0, len(records))
	for _, r := range records {
		segment := r.Obj("segment")
		document := segment.Obj("document")
		hits = append(hits, &entity.RetrievalHit{
			Score:        r.Float("score"),
			Segment:      SegmentFrom(segment),
			DocumentID:   document.Str("id"),
			DocumentName: document.Str("name"),
		})
	}
	return hits
}

func SegmentFrom(o kernel.Object) *entity.Segment {
	return &entity.Segment{
		ID:        o.Str("id"),
		Content:   o.Str("content"),
		Answer:    o.Str("answer"),
		Keywords:  o.Strs("keywords"),
		Position:  o.Int("position"),
		WordCount: o.Int("word_count"),
		Enabled:   boolOr(o, "enabled", true),
		Raw:       o.Raw(),
	}
}

// UnwrapData is the "data" object inside an envelope, or the envelope itself
// when there is no such wrapping.
func UnwrapData(o kernel.Object) map[string]any {
	if data := o.Obj("data"); len(data) > 0 {
		return data.Raw()
	}
	return o.Raw()
}

func TagFrom(o kernel.Object) *entity.Tag {
	count := o.Str("binding_count")
	n := 0
	if count != "" {
		digits := true
		for _, c := range count {
			if c < '0' || c > '9' {
				digits = false
				break
			}
		}
		if digits {
			for _, c := range count {
				n = n*10 + int(c-'0')
			}
		}
	}
	return &entity.Tag{
		ID:           o.Str("id"),
		Name:         o.Str("name"),
		Type:         o.Str("type"),
		BindingCount: n,
		Raw:          o.Raw(),
	}
}

// LabelOf reads one readable label out of Dify's {"en_US": …, "zh_Hans": …},
// falling back to whatever language is there rather than to an empty string:
// a provider labelled only in Chinese still has a name.
func LabelOf(value any) string {
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

func modelFrom(o kernel.Object, provider string) *entity.Model {
	return &entity.Model{
		Name:       o.Str("model"),
		Provider:   provider,
		Label:      LabelOf(o["label"]),
		Type:       o.Str("model_type"),
		Status:     o.Str("status"),
		Deprecated: o.Bool("deprecated"),
		Properties: o.Obj("model_properties").Raw(),
		Raw:        o.Raw(),
	}
}

// providersFrom shapes a model listing, from either the Service API or the
// console.
func providersFrom(items []any) []*entity.ModelProvider {
	out := make([]*entity.ModelProvider, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		o := kernel.Object(m)
		provider := o.Str("provider")
		var models []*entity.Model
		for _, mp := range o.Objs("models") {
			models = append(models, modelFrom(mp, provider))
		}
		out = append(out, &entity.ModelProvider{
			Provider: provider,
			Label:    LabelOf(o["label"]),
			Status:   o.Str("status"),
			Models:   models,
			Raw:      o.Raw(),
		})
	}
	return out
}

func TagsFrom(o kernel.Object) []*entity.Tag { return buildAll(o.Objs("data"), TagFrom) }

func SegmentsFrom(o kernel.Object) []*entity.Segment { return buildAll(o.Objs("data"), SegmentFrom) }

func SegmentFromEnvelope(o kernel.Object) *entity.Segment {
	return SegmentFrom(kernel.Object(UnwrapData(o)))
}

// RetrievedDocumentFrom reads a document back from whichever envelope this
// Dify used: "data", "document", or none.
func RetrievedDocumentFrom(o kernel.Object) *entity.Document {
	data := o.Obj("data")
	if len(data) == 0 {
		data = o.Obj("document")
	}
	if len(data) == 0 {
		data = o
	}
	return DocumentFrom(data)
}

func DownloadURLFrom(o kernel.Object) string { return o.Str("url") }

// MetadataFieldsFrom reads a field listing under key, or under "data" where
// a Dify answers with the generic envelope instead.
func MetadataFieldsFrom(o kernel.Object, key string) []*entity.MetadataField {
	items := o.Objs(key)
	if len(items) == 0 {
		items = o.Objs("data")
	}
	return buildAll(items, MetadataFieldFrom)
}

func DataMaps(o kernel.Object) []map[string]any { return o.Maps("data") }

func ModelProvidersFrom(o kernel.Object) []*entity.ModelProvider {
	return providersFrom(o.List("data"))
}

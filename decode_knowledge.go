package dify

func datasetFrom(o object) *Dataset {
	return &Dataset{
		ID:                o.str("id"),
		Name:              o.str("name"),
		Description:       o.str("description"),
		Permission:        o.str("permission"),
		IndexingTechnique: o.str("indexing_technique"),
		DocumentCount:     o.int("document_count"),
		WordCount:         o.int("word_count"),
		AppCount:          o.int("app_count"),
		Raw:               o.raw(),
	}
}

// boolOr reads a boolean that defaults to true when absent — Dify's own
// "enabled" field on a document or segment, which is true unless explicitly
// turned off.
func boolOr(o object, key string, def bool) bool {
	if !o.has(key) {
		return def
	}
	return o.bool(key)
}

func documentFrom(o object) *Document {
	return &Document{
		ID:             o.str("id"),
		Name:           o.str("name"),
		IndexingStatus: o.str("indexing_status"),
		WordCount:      o.int("word_count"),
		Enabled:        boolOr(o, "enabled", true),
		Error:          o.str("error"),
		Batch:          o.str("batch"),
		Raw:            o.raw(),
	}
}

// createdDocumentFrom shapes a document as Create and Update report it. Dify
// nests the document under "document" and puts the indexing batch beside it,
// not inside it — so the batch has to be carried across, or the document
// cannot be asked about its own indexing.
func createdDocumentFrom(o object) *Document {
	doc := o.obj("document")
	if len(doc) == 0 {
		doc = o
	}
	batch := firstNonZero(o.str("batch"), doc.str("batch"))
	merged := make(object, len(doc)+1)
	for k, v := range doc {
		merged[k] = v
	}
	merged["batch"] = batch
	return documentFrom(merged)
}

// statusFrom shapes an indexing-status answer, which arrives as a
// one-item array.
func statusFrom(o object) *IndexingStatus {
	items := o.objs("data")
	data := object{}
	if len(items) > 0 {
		data = items[0]
	}
	return &IndexingStatus{
		ID:                data.str("id"),
		Status:            data.str("indexing_status"),
		CompletedSegments: data.int("completed_segments"),
		TotalSegments:     data.int("total_segments"),
		Error:             data.str("error"),
	}
}

func metadataFieldFrom(o object) *MetadataField {
	return &MetadataField{
		Name:  o.str("name"),
		Type:  o.str("type"),
		ID:    o.str("id"),
		Count: o.int("count"),
		Raw:   o.raw(),
	}
}

func ingestionFrom(o object, datasetID string) *PipelineIngestion {
	batch := o.str("batch")
	documents := o.objs("documents")
	docs := make([]*Document, 0, len(documents))
	for _, item := range documents {
		merged := make(object, len(item)+1)
		for k, v := range item {
			merged[k] = v
		}
		merged["batch"] = batch
		docs = append(docs, documentFrom(merged))
	}
	id := o.obj("dataset").str("id")
	if id == "" {
		id = datasetID
	}
	return &PipelineIngestion{Batch: batch, Documents: docs, DatasetID: id, Raw: o.raw()}
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

func segmentFrom(o object) *Segment {
	return &Segment{
		ID:        o.str("id"),
		Content:   o.str("content"),
		Answer:    o.str("answer"),
		Keywords:  o.strs("keywords"),
		Position:  o.int("position"),
		WordCount: o.int("word_count"),
		Enabled:   boolOr(o, "enabled", true),
		Raw:       o.raw(),
	}
}

// unwrapData is the "data" object inside an envelope, or the envelope itself
// when there is no such wrapping.
func unwrapData(o object) map[string]any {
	if data := o.obj("data"); len(data) > 0 {
		return data.raw()
	}
	return o.raw()
}

func tagFrom(o object) *Tag {
	count := o.str("binding_count")
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
	return &Tag{
		ID:           o.str("id"),
		Name:         o.str("name"),
		Type:         o.str("type"),
		BindingCount: n,
		Raw:          o.raw(),
	}
}

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

func tagsFrom(o object) []*Tag { return buildAll(o.objs("data"), tagFrom) }

func segmentsFrom(o object) []*Segment { return buildAll(o.objs("data"), segmentFrom) }

func segmentFromEnvelope(o object) *Segment { return segmentFrom(object(unwrapData(o))) }

// retrievedDocumentFrom reads a document back from whichever envelope this
// Dify used: "data", "document", or none.
func retrievedDocumentFrom(o object) *Document {
	data := o.obj("data")
	if len(data) == 0 {
		data = o.obj("document")
	}
	if len(data) == 0 {
		data = o
	}
	return documentFrom(data)
}

func downloadURLFrom(o object) string { return o.str("url") }

// metadataFieldsFrom reads a field listing under key, or under "data" where
// a Dify answers with the generic envelope instead.
func metadataFieldsFrom(o object, key string) []*MetadataField {
	items := o.objs(key)
	if len(items) == 0 {
		items = o.objs("data")
	}
	return buildAll(items, metadataFieldFrom)
}

func dataMaps(o object) []map[string]any { return o.maps("data") }

func modelProvidersFrom(o object) []*ModelProvider { return providersFrom(o.list("data")) }

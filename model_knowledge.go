package dify

// Dataset is a knowledge base.
type Dataset struct {
	ID                string
	Name              string
	Description       string
	Permission        string
	IndexingTechnique string
	DocumentCount     int
	WordCount         int
	AppCount          int
	Raw               map[string]any
}

func (d *Dataset) String() string { return d.Name }

// Document is one document inside a dataset.
type Document struct {
	ID             string
	Name           string
	IndexingStatus string
	WordCount      int
	Enabled        bool
	Error          string
	// Batch is the indexing batch this document arrived in, carried across
	// from Create or Update — a document read back with List or Retrieve
	// does not have one. IndexingStatus, WaitUntilSettled and
	// WaitUntilIndexed all take this, not the document id.
	Batch string
	Raw   map[string]any
}

// Indexed reports whether the document has finished indexing and is
// searchable.
func (d *Document) Indexed() bool { return d.IndexingStatus == "completed" }

func (d *Document) String() string { return d.Name }

// IndexingStatus is how far the indexing of one batch has got. Uploading a
// document returns before it is searchable; this is what says when it is.
type IndexingStatus struct {
	ID                string
	Status            string
	CompletedSegments int
	TotalSegments     int
	Error             string
}

// Finished reports whether indexing has stopped, however it stopped.
func (s *IndexingStatus) Finished() bool {
	switch s.Status {
	case "completed", "error", "paused":
		return true
	}
	return false
}

// Indexed reports whether indexing finished successfully.
func (s *IndexingStatus) Indexed() bool { return s.Status == "completed" }

// MetadataField is a field documents in one knowledge base may carry.
//
// Two kinds arrive in this shape and the difference is the id: a field you
// defined has one and can be renamed or deleted by it, while one of Dify's
// own — filename, upload date — has none, because those are turned on and
// off rather than managed.
type MetadataField struct {
	Name string
	Type string
	ID   string
	// Count is how many documents have a value for it. Absent (zero) on
	// Dify's own fields.
	Count int
	Raw   map[string]any
}

// BuiltIn reports whether Dify fills this field in rather than you.
func (m *MetadataField) BuiltIn() bool { return m.ID == "" }

func (m *MetadataField) String() string { return m.Name }

// PipelineIngestion is what running a published pipeline queued.
//
// A published run does not answer with the work: it enqueues one document
// per source and answers with the batch they share. The documents are not
// indexed yet — Documents.IndexingStatus(batch) is how far it has got, and
// Documents.WaitUntilIndexed waits for it.
type PipelineIngestion struct {
	Batch     string
	Documents []*Document
	DatasetID string
	Raw       map[string]any
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

// Segment is a chunk of a document, as retrieval sees it.
type Segment struct {
	ID        string
	Content   string
	Answer    string
	Keywords  []string
	Position  int
	WordCount int
	Enabled   bool
	Raw       map[string]any
}

func (s *Segment) String() string { return s.Content }

// Tag is a label across the workspace's knowledge bases.
type Tag struct {
	ID   string
	Name string
	Type string
	// BindingCount is how many knowledge bases carry it. Dify sends this as a
	// *string* on the wire, so comparing the raw payload's value to an int
	// compares a str to an int; this is the parsed form.
	BindingCount int
	Raw          map[string]any
}

func (t *Tag) String() string { return t.Name }

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

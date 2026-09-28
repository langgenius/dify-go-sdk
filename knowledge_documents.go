package dify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Documents are the documents inside one knowledge base.
//
//	docs := knowledge.Documents(dataset.ID)
//	doc, err := docs.CreateFromText(ctx, "notes", "…", nil)
//	docs.WaitUntilIndexed(ctx, doc.Batch, nil)
type Documents struct {
	api       port
	datasetID string
}

func (d *Documents) path(parts ...string) string { return datasetPath(d.datasetID, parts...) }

func processRuleOrAutomatic(rule map[string]any) map[string]any {
	if rule == nil {
		return map[string]any{"mode": "automatic"}
	}
	return rule
}

// DocumentCreateParams are the optional parts of adding a document.
type DocumentCreateParams struct {
	// IndexingTechnique defaults to "high_quality". Required by Dify only
	// when adding the first document to a knowledge base with none set yet;
	// sent every time here since Dify accepts it regardless.
	IndexingTechnique string
	// ProcessRule defaults to {"mode": "automatic"}.
	ProcessRule map[string]any
	// DocForm is "text_model" (default), "hierarchical_model" for
	// parent-child chunks, or "qa_model".
	DocForm     string
	DocLanguage string
	// Embedding overrides the model, spelled "provider/plugin/name:model".
	Embedding string
	// Retrieval overrides the knowledge base's own retrieval settings for
	// this document's chunks, built with RetrievalModel.
	Retrieval map[string]any
	// OriginalDocumentID replaces that document's content instead of adding
	// a new one.
	OriginalDocumentID string
}

func createSettings(p *DocumentCreateParams) (map[string]any, error) {
	if p == nil {
		p = &DocumentCreateParams{}
	}
	body := map[string]any{
		"indexing_technique": firstNonZero(p.IndexingTechnique, "high_quality"),
		"process_rule":       processRuleOrAutomatic(p.ProcessRule),
	}
	if p.DocForm != "" {
		body["doc_form"] = p.DocForm
	}
	if p.DocLanguage != "" {
		body["doc_language"] = p.DocLanguage
	}
	if p.Retrieval != nil {
		body["retrieval_model"] = p.Retrieval
	}
	if p.OriginalDocumentID != "" {
		body["original_document_id"] = p.OriginalDocumentID
	}
	if p.Embedding != "" {
		provider, model, err := splitModel(p.Embedding, "embedding")
		if err != nil {
			return nil, err
		}
		body["embedding_model_provider"] = provider
		body["embedding_model"] = model
	}
	return body, nil
}

// CreateFromText adds a document made of text. Uses Dify's canonical
// hyphenated route (create-by-text); the underscored spelling is the same
// operation, marked deprecated.
//
// Returns before indexing finishes; the document is not searchable until it
// does. WaitUntilIndexed waits, IndexingStatus asks.
func (d *Documents) CreateFromText(ctx context.Context, name, text string, p *DocumentCreateParams) (*Document, error) {
	body, err := createSettings(p)
	if err != nil {
		return nil, err
	}
	body["name"] = firstNonZero(name, "document")
	body["text"] = text
	o, err := d.api.call(ctx, &request{method: http.MethodPost, path: d.path("document", "create-by-text"), body: body})
	if err != nil {
		return nil, err
	}
	return createdDocumentFrom(o), nil
}

// CreateFromFile adds a document uploaded from a file. Uses Dify's canonical
// hyphenated route (create-by-file).
func (d *Documents) CreateFromFile(ctx context.Context, file Upload, p *DocumentCreateParams) (*Document, error) {
	body, err := createSettings(p)
	if err != nil {
		return nil, err
	}
	// Not requireType: Dify picks the reader by the file's extension, not by
	// its declared content type, so an untyped upload is not refused here.
	part, err := file.part("file", false)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("dify: encoding document settings: %w", err)
	}
	o, err := d.api.call(ctx, &request{method: http.MethodPost, path: d.path("document", "create-by-file"), form: &multipartForm{fields: map[string]string{"data": string(data)}, file: part}})
	if err != nil {
		return nil, err
	}
	return createdDocumentFrom(o), nil
}

// DocumentListParams narrow a knowledge base's document listing.
type DocumentListParams struct {
	Page    int
	Limit   int
	Keyword string
	// Status is one display status: queuing, indexing, paused, error,
	// available, disabled or archived.
	Status string
}

// List lists this knowledge base's documents.
func (d *Documents) List(ctx context.Context, p *DocumentListParams) (*Page[*Document], error) {
	if p == nil {
		p = &DocumentListParams{}
	}
	fetch := func(ctx context.Context, number int) (object, error) {
		q := params{}.setInt("page", number).setInt("limit", p.Limit).set("keyword", p.Keyword).set("status", p.Status)
		return d.api.call(ctx, &request{method: http.MethodGet, path: d.path("documents"), query: q.values()})
	}
	return fetchByPage(ctx, documentFrom, fetch, p.Page)
}

// DocumentUpdateTextParams are the optional parts of replacing a document's
// text.
type DocumentUpdateTextParams struct {
	// Name defaults to the document's current name when left empty — Dify
	// requires a name alongside text and answers a bare validation error
	// rather than saying which field it wants, so this fetches the document
	// first rather than surfacing that.
	Name        string
	ProcessRule map[string]any
	DocForm     string
	DocLanguage string
	Retrieval   map[string]any
}

// UpdateFromText replaces a document's text content. Dify's payload for this
// route has no embedding_model field — unlike UpdateFromFile — so switching
// the embedding model on a text update is not a thing this route does.
func (d *Documents) UpdateFromText(ctx context.Context, documentID, text string, p *DocumentUpdateTextParams) (*Document, error) {
	if p == nil {
		p = &DocumentUpdateTextParams{}
	}
	name := p.Name
	if name == "" {
		current, err := d.Retrieve(ctx, documentID)
		if err != nil {
			return nil, err
		}
		name = current.Name
	}
	body := map[string]any{"text": text, "name": name}
	if p.ProcessRule != nil {
		body["process_rule"] = p.ProcessRule
	}
	if p.DocForm != "" {
		body["doc_form"] = p.DocForm
	}
	if p.DocLanguage != "" {
		body["doc_language"] = p.DocLanguage
	}
	if p.Retrieval != nil {
		body["retrieval_model"] = p.Retrieval
	}
	o, err := d.api.call(ctx, &request{method: http.MethodPost, path: d.path("documents", pathEscape(documentID), "update-by-text"), body: body})
	if err != nil {
		return nil, err
	}
	return createdDocumentFrom(o), nil
}

// DocumentUpdateFileParams are the optional parts of replacing a document's
// file.
type DocumentUpdateFileParams struct {
	ProcessRule map[string]any
	DocForm     string
	DocLanguage string
	Retrieval   map[string]any
	Embedding   string
}

// UpdateFromFile replaces a document's file content. Sent as a PATCH on the
// document itself: Dify marks *both* spellings of "update-by-file"
// deprecated, and this route is the one that replaces them.
func (d *Documents) UpdateFromFile(ctx context.Context, documentID string, file Upload, p *DocumentUpdateFileParams) (*Document, error) {
	if p == nil {
		p = &DocumentUpdateFileParams{}
	}
	body := map[string]any{}
	if p.ProcessRule != nil {
		body["process_rule"] = p.ProcessRule
	}
	if p.DocForm != "" {
		body["doc_form"] = p.DocForm
	}
	if p.DocLanguage != "" {
		body["doc_language"] = p.DocLanguage
	}
	if p.Retrieval != nil {
		body["retrieval_model"] = p.Retrieval
	}
	if p.Embedding != "" {
		provider, model, err := splitModel(p.Embedding, "embedding")
		if err != nil {
			return nil, err
		}
		body["embedding_model_provider"] = provider
		body["embedding_model"] = model
	}
	part, err := file.part("file", false)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("dify: encoding document settings: %w", err)
	}
	o, err := d.api.call(ctx, &request{method: http.MethodPatch, path: d.path("documents", pathEscape(documentID)), form: &multipartForm{fields: map[string]string{"data": string(data)}, file: part}})
	if err != nil {
		return nil, err
	}
	return createdDocumentFrom(o), nil
}

// Retrieve reads one document back, with its current indexing state.
func (d *Documents) Retrieve(ctx context.Context, documentID string) (*Document, error) {
	o, err := d.api.call(ctx, &request{method: http.MethodGet, path: d.path("documents", pathEscape(documentID))})
	if err != nil {
		return nil, err
	}
	return retrievedDocumentFrom(o), nil
}

// Delete removes a document and its segments.
func (d *Documents) Delete(ctx context.Context, documentID string) error {
	_, err := d.api.call(ctx, &request{method: http.MethodDelete, path: d.path("documents", pathEscape(documentID))})
	return err
}

// IndexingStatus reports how far indexing has got, for the batch a document
// arrived in — not the document's own id. Get it from Document.Batch on
// what CreateFromText, CreateFromFile, UpdateFromText or UpdateFromFile
// returned; a document read back with List or Retrieve does not carry one.
func (d *Documents) IndexingStatus(ctx context.Context, batch string) (*IndexingStatus, error) {
	if batch == "" {
		return nil, argError("this document carries no indexing batch, so there is nothing to ask about. Dify reports one when a document is created or updated; a document read back from List or Retrieve does not have it.")
	}
	o, err := d.api.call(ctx, &request{method: http.MethodGet, path: d.path("documents", pathEscape(batch), "indexing-status")})
	if err != nil {
		return nil, err
	}
	return statusFrom(o), nil
}

// WaitParams control how long WaitUntilSettled and WaitUntilIndexed poll.
type WaitParams struct {
	// Timeout defaults to 120 seconds.
	Timeout time.Duration
	// Poll defaults to one second between checks.
	Poll time.Duration
}

// WaitUntilSettled blocks until indexing stops, however it stops — including
// "error" and "paused". Use this when the outcome itself is what you want to
// inspect; WaitUntilIndexed when you want to carry on only if it worked.
func (d *Documents) WaitUntilSettled(ctx context.Context, batch string, p *WaitParams) (*IndexingStatus, error) {
	if p == nil {
		p = &WaitParams{}
	}
	timeout := firstNonZero(p.Timeout, 120*time.Second)
	poll := firstNonZero(p.Poll, time.Second)
	deadline := time.Now().Add(timeout)
	for {
		status, err := d.IndexingStatus(ctx, batch)
		if err != nil {
			return nil, err
		}
		if status.Finished() {
			return status, nil
		}
		if time.Now().After(deadline) {
			return status, fmt.Errorf("dify: indexing was still %q after %s (%d/%d segments) for batch %s. Poll IndexingStatus yourself for a longer wait.", status.Status, timeout, status.CompletedSegments, status.TotalSegments, batch)
		}
		if err := sleepCtx(ctx, poll); err != nil {
			return nil, err
		}
	}
}

// WaitUntilIndexed blocks until the document is **searchable**, or returns an
// error naming the document.
//
// A document is not retrievable the moment it uploads, which is the usual
// surprise when a freshly added one returns no hits. Indexing that stopped
// without finishing — error, or paused because the workspace ran out of
// quota — errors rather than returning success; WaitUntilSettled returns
// that state instead of raising, when the outcome itself is what is wanted.
func (d *Documents) WaitUntilIndexed(ctx context.Context, batch string, p *WaitParams) (*IndexingStatus, error) {
	settled, err := d.WaitUntilSettled(ctx, batch, p)
	if err != nil {
		return settled, err
	}
	if settled.Indexed() {
		return settled, nil
	}
	detail := ""
	if settled.Error != "" {
		detail = ": " + settled.Error
	}
	return settled, fmt.Errorf("dify: indexing for batch %s stopped at %q rather than completing%s (%d/%d segments). The document is not searchable.", batch, settled.Status, detail, settled.CompletedSegments, settled.TotalSegments)
}

// DocumentStatus is an action Documents.SetStatus performs in bulk.
type DocumentStatus string

const (
	DocumentEnable    DocumentStatus = "enable"
	DocumentDisable   DocumentStatus = "disable"
	DocumentArchive   DocumentStatus = "archive"
	DocumentUnarchive DocumentStatus = "un_archive"
)

// SetStatus enables, disables, archives or unarchives documents in bulk,
// without deleting them.
func (d *Documents) SetStatus(ctx context.Context, documentIDs []string, action DocumentStatus) error {
	_, err := d.api.call(ctx, &request{method: http.MethodPatch, path: d.path("documents", "status", string(action)), body: map[string]any{"document_ids": documentIDs}})
	return err
}

// SetEnabled turns documents on or off for retrieval, without deleting them —
// a convenience over SetStatus for the enable/disable pair.
func (d *Documents) SetEnabled(ctx context.Context, documentIDs []string, enabled bool) error {
	action := DocumentDisable
	if enabled {
		action = DocumentEnable
	}
	return d.SetStatus(ctx, documentIDs, action)
}

// DownloadURL is a signed URL to one document's original uploaded file.
//
// Dify's Service API answers this route with {"url": "..."} rather than the
// file's bytes. Fetch the URL yourself with a plain HTTP client; it is
// presigned and needs no Dify credential.
func (d *Documents) DownloadURL(ctx context.Context, documentID string) (string, error) {
	o, err := d.api.call(ctx, &request{method: http.MethodGet, path: d.path("documents", pathEscape(documentID), "download")})
	if err != nil {
		return "", err
	}
	return downloadURLFrom(o), nil
}

// DownloadAll downloads several documents at once, as a zip. A POST, not a
// GET, and the ids are required rather than meaning "all": a knowledge base
// can hold more of them than a URL would carry, and Dify will not guess at
// which ones you meant.
func (d *Documents) DownloadAll(ctx context.Context, documentIDs []string) ([]byte, error) {
	if len(documentIDs) == 0 {
		return nil, argError("name the documents to download. Dify has no download-everything; List then pass what you want.")
	}
	raw, _, err := d.api.bytes(ctx, &request{method: http.MethodPost, path: d.path("documents", "download-zip"), body: map[string]any{"document_ids": documentIDs}})
	return raw, err
}

// SetMetadata writes metadata onto documents in bulk. Each operation is
// {"document_id": ..., "metadata_list": [{"id": ..., "value": ...}, ...]}.
func (d *Documents) SetMetadata(ctx context.Context, operations []map[string]any) error {
	_, err := d.api.call(ctx, &request{method: http.MethodPost, path: d.path("documents", "metadata"), body: map[string]any{"operation_data": operations}})
	return err
}

// Segments is the chunks of one document.
func (d *Documents) Segments(documentID string) *Segments {
	return &Segments{api: d.api, datasetID: d.datasetID, documentID: documentID}
}

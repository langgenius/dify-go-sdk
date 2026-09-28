package dify

// The types and functions a caller uses are declared in the internal layers
// and re-declared here, so that everything is dify.X. Each name below is the
// same type or the same value as the one it points at; the doc comments are
// copied from there, and the fields and methods are documented at the
// declaration.

import (
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/langgenius/dify-go-sdk/internal/codec"
	"github.com/langgenius/dify-go-sdk/internal/entity"
	"github.com/langgenius/dify-go-sdk/internal/infra"
	"github.com/langgenius/dify-go-sdk/internal/kernel"
	"github.com/langgenius/dify-go-sdk/internal/usecase"
)

// internal/usecase/annotations.go

// Annotations are this app's annotations, and the reply setting that uses
// them.
type Annotations = usecase.Annotations

// AnnotationListParams narrow the annotations.
type AnnotationListParams = usecase.AnnotationListParams

// ReplySettings configure annotation reply. Dify's payload model requires all
// three fields on enable and on disable alike, so a disable has to name them
// too; leaving them out answers 422.
type ReplySettings = usecase.ReplySettings

// internal/usecase/app.go

// App is one Dify app, addressed by its Service-API key.
//
// The client holds the connection and the credential. What you can do hangs
// off it by what it acts on:
//
//	app, err := dify.NewApp(dify.WithAPIKey("app-…"), dify.WithUser("alice"))
//	msg, err := app.Chat.Messages.Create(ctx, "Hello", nil)
//	run, err := app.Workflows.Runs.Create(ctx, map[string]any{"text": "…"}, nil)
//
// An app has one mode, and Dify serves each mode on its own route — so
// app.Chat on a workflow app, or app.Workflows on a chatflow, answers "check
// if your app mode matches the right API route". Info reports which this is.
//
// An App is safe for concurrent use.
type App = usecase.App

// Chat is the conversational half of an app: messages and the threads they
// form.
type Chat = usecase.Chat

// Workflows is the workflow half of an app: runs, and what they did.
type Workflows = usecase.Workflows

// PageParams pages a numbered listing. Zero values take Dify's defaults.
type PageParams = usecase.PageParams

// internal/usecase/audio.go

// Audio is text to speech, and speech to text, for this app.
type Audio = usecase.Audio

// SpeakParams choose what is spoken and how.
type SpeakParams = usecase.SpeakParams

// internal/usecase/completions.go

// CompletionParams are the optional parts of a completion.
type CompletionParams = usecase.CompletionParams

// Completions are one prompt in, one answer out, no thread. Only a
// completion-mode app serves these.
type Completions = usecase.Completions

// internal/usecase/conversations.go

// Conversations are a user's threads with this app.
type Conversations = usecase.Conversations

// ConversationListParams narrow a user's conversations.
type ConversationListParams = usecase.ConversationListParams

// RenameParams choose how a thread is renamed.
type RenameParams = usecase.RenameParams

// VariableParams narrow a thread's variables.
type VariableParams = usecase.VariableParams

// internal/usecase/files.go

// Upload is something to upload: a name, its bytes, and its type.
type Upload = usecase.Upload

// FileFromPath uploads the file at path, under its base name.
func FileFromPath(path string) Upload { return usecase.FileFromPath(path) }

// FileFromReader uploads what r yields, recorded as name.
func FileFromReader(name string, r io.Reader) Upload { return usecase.FileFromReader(name, r) }

// Files are the files this app's runs and messages can reference.
type Files = usecase.Files

// internal/usecase/forms.go

// Forms are the forms a paused run is waiting on.
//
// Waiting is not failing: a run that reaches a human-input node stops, its
// stream ends, and it resumes when the form comes back. WorkflowRun.Paused
// says so, and PendingForms carries the token.
type Forms = usecase.Forms

// FormToken picks the token to answer a paused run with — the first of its
// PendingForms — or explains why there is none.
func FormToken(run *WorkflowRun) (string, error) { return usecase.FormToken(run) }

// internal/usecase/knowledge.go

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
type Knowledge = usecase.Knowledge

// internal/usecase/knowledge_datasets.go

// Datasets are the workspace's knowledge bases.
type Datasets = usecase.Datasets

// DatasetCreateParams are the optional parts of creating a knowledge base.
type DatasetCreateParams = usecase.DatasetCreateParams

// DatasetListParams narrow the workspace's knowledge base listing.
type DatasetListParams = usecase.DatasetListParams

// DatasetUpdateParams are the changeable settings of a knowledge base. A nil
// field is left as it was; only what is set here is sent, since Dify updates
// only the fields present in the PATCH body.
type DatasetUpdateParams = usecase.DatasetUpdateParams

// SearchParams are the optional parts of a retrieval call.
type SearchParams = usecase.SearchParams

// internal/usecase/knowledge_documents.go

// Documents are the documents inside one knowledge base.
//
//	docs := knowledge.Documents(dataset.ID)
//	doc, err := docs.CreateFromText(ctx, "notes", "…", nil)
//	docs.WaitUntilIndexed(ctx, doc.Batch, nil)
type Documents = usecase.Documents

// DocumentCreateParams are the optional parts of adding a document.
type DocumentCreateParams = usecase.DocumentCreateParams

// DocumentListParams narrow a knowledge base's document listing.
type DocumentListParams = usecase.DocumentListParams

// DocumentUpdateTextParams are the optional parts of replacing a document's
// text.
type DocumentUpdateTextParams = usecase.DocumentUpdateTextParams

// DocumentUpdateFileParams are the optional parts of replacing a document's
// file.
type DocumentUpdateFileParams = usecase.DocumentUpdateFileParams

// WaitParams control how long WaitUntilSettled and WaitUntilIndexed poll.
type WaitParams = usecase.WaitParams

// DocumentStatus is an action Documents.SetStatus performs in bulk.
type DocumentStatus = usecase.DocumentStatus

const (
	DocumentEnable    = usecase.DocumentEnable
	DocumentDisable   = usecase.DocumentDisable
	DocumentArchive   = usecase.DocumentArchive
	DocumentUnarchive = usecase.DocumentUnarchive
)

// internal/usecase/knowledge_pipeline.go

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
type Pipeline = usecase.Pipeline

// DatasourcesParams controls whether Datasources reads the published or
// draft pipeline.
type DatasourcesParams = usecase.DatasourcesParams

// RunDatasourceParams are the optional parts of running one datasource node.
type RunDatasourceParams = usecase.RunDatasourceParams

// PipelineRunInput is what a pipeline run needs, published or draft. Every
// field but Inputs is required, which is why this is a plain struct rather
// than a *Params one — there is no sane default for "which datasource, from
// where".
type PipelineRunInput = usecase.PipelineRunInput

// internal/usecase/knowledge_search.go

// How Dify may search a knowledge base.
const (
	SearchSemantic = usecase.SearchSemantic
	SearchFullText = usecase.SearchFullText
	SearchHybrid   = usecase.SearchHybrid
	SearchKeyword  = usecase.SearchKeyword
)

// WeightedScoreParams builds the weights block for hybrid search.
type WeightedScoreParams = usecase.WeightedScoreParams

// WeightedScore blends vector and keyword scores instead of calling a rerank
// model:
//
//	weights, err := dify.WeightedScore(dify.WeightedScoreParams{Embedding: embeddingModel})
//	retrieval, err := dify.RetrievalModel(&dify.RetrievalModelParams{Search: dify.SearchHybrid, Weights: weights})
func WeightedScore(p WeightedScoreParams) (map[string]any, error) { return usecase.WeightedScore(p) }

// RetrievalModelParams builds the retrieval_model block a knowledge base is
// searched by. How a knowledge base is searched is stored on the base, not
// passed per call — Datasets.Create's Retrieval field decides what every
// later retrieval does, a workflow's knowledge node included. Three things
// about that block are easy to get wrong, and all three are read out of
// Dify's retrieval code rather than its documentation:
//
//   - A score threshold is two fields. score_threshold is ignored unless
//     score_threshold_enabled is true, so a threshold set alone reads as a
//     filter that does nothing. Setting ScoreThreshold turns the flag on;
//     leaving it nil turns it off — those are the two states.
//   - An "economy" knowledge base is always searched by keyword, whatever
//     Search says: it has no embeddings to compare against. The setting is
//     kept because the base can be switched to "high_quality" later.
//   - Reranking has two modes and only one calls a model. Dify reads
//     reranking_model only when reranking_enable is true; Weights instead
//     blends the vector and keyword scores arithmetically, with no model
//     call. Setting both Rerank and Weights is not a stronger rerank, it is a
//     contradiction, and is refused here. On a knowledge base's own settings
//     (as opposed to a workflow's knowledge node over several bases),
//     hybrid search reads the weights whatever reranking_enable says, so
//     Weights leaves it off.
type RetrievalModelParams = usecase.RetrievalModelParams

// RetrievalModel builds the retrieval_model block. See RetrievalModelParams
// for what each setting does and the two reranking modes it refuses to
// combine.
func RetrievalModel(p *RetrievalModelParams) (map[string]any, error) {
	return usecase.RetrievalModel(p)
}

// internal/usecase/knowledge_segments.go

// Segments are the chunks of one document, and the child chunks beneath
// them.
type Segments = usecase.Segments

// SegmentListParams narrow a document's segment listing.
type SegmentListParams = usecase.SegmentListParams

// ChildChunkListParams narrow a segment's child-chunk listing.
type ChildChunkListParams = usecase.ChildChunkListParams

// internal/usecase/knowledge_tags.go

// Tags are tags across the workspace's knowledge bases.
//
// Workspace-level, not per-dataset: a tag exists once and is bound to as
// many knowledge bases as you like. Datasets.Tags reads the other
// direction — which tags one base carries.
type Tags = usecase.Tags

// internal/usecase/messages.go

// MessageParams are the optional parts of sending a chat message.
type MessageParams = usecase.MessageParams

// Messages are the messages in this app's conversations.
//
// Create waits for the whole answer. Stream hands it back as it is written.
// Both return the thread's ConversationID, which is what continues it.
type Messages = usecase.Messages

// HistoryParams narrow a conversation's history.
type HistoryParams = usecase.HistoryParams

// Rating is feedback on a message.
type Rating = usecase.Rating

const (
	Like    = usecase.Like
	Dislike = usecase.Dislike
	// NoRating takes a rating back.
	NoRating = usecase.NoRating
)

// FeedbackParams are the optional parts of rating a message.
type FeedbackParams = usecase.FeedbackParams

// internal/usecase/runs.go

// RunParams are the optional parts of starting a workflow run.
type RunParams = usecase.RunParams

// WorkflowRuns are runs of this app's workflow.
//
// Create waits for the run and returns it. Stream hands it back as it
// happens. Retrieve reads one back afterwards, and Stop ends one still going.
type WorkflowRuns = usecase.WorkflowRuns

// EventsParams are the optional parts of reopening a run's stream.
type EventsParams = usecase.EventsParams

// LogParams narrow the run history.
type LogParams = usecase.LogParams

// internal/entity/model_app.go

// NodeExecution is one execution of one node.
//
// Distinct from the node itself: a node inside an iteration or a loop runs
// once per item, and each pass is its own execution with its own usage.
// Treating the two as the same thing is what made a looped node report only
// its last pass.
type NodeExecution = entity.NodeExecution

// WorkflowRun is one run of a workflow, as Dify reported it.
//
// Succeeded, Paused and Failed are three separate questions: a run waiting
// for a person has neither succeeded nor failed.
type WorkflowRun = entity.WorkflowRun

// Message is one message a chat or completion app produced, and the thread it
// belongs to.
type Message = entity.Message

// ServerInfo is what the Service API says about itself, before any
// credential.
type ServerInfo = entity.ServerInfo

// AppInfo is what Dify says an app is.
type AppInfo = entity.AppInfo

// InputField is one field an app declares, as its start node defines it.
//
// Name is the key to put in inputs. Dify spells it "variable" and puts a
// separate label beside it for display; using the label as the key is the
// usual first mistake, because for a field created in the UI the two are
// often the same string and it works until someone renames one.
type InputField = entity.InputField

// AppParameters is what an app declares it takes, and what it has turned on.
type AppParameters = entity.AppParameters

// SiteSettings is the WebApp's own settings: what a visitor sees before
// typing anything.
type SiteSettings = entity.SiteSettings

// Annotation is a question and the answer you want given for it.
type Annotation = entity.Annotation

// AnnotationReplyJob is the indexing job that turns annotation reply on or
// off. Enabling it embeds every annotation, which takes time — so Dify
// answers with a job rather than a result.
type AnnotationReplyJob = entity.AnnotationReplyJob

// Conversation is one thread, belonging to one user.
type Conversation = entity.Conversation

// UploadedFile is a file Dify has taken, and the reference that points at it.
type UploadedFile = entity.UploadedFile

// Form is a paused run's human-input form, as it should be shown to whoever
// fills it in.
type Form = entity.Form

// HistoryMessage is one turn in a conversation, as Dify records it.
//
// Deliberately not a Message. History carries the Query that prompted the
// answer, the files attached, the feedback left and what it cost — none of
// which a fresh reply has — and it has no TaskID, because nothing is running
// to stop. Reusing one type for both dropped the query, leaving a transcript
// of answers to questions nobody could see.
type HistoryMessage = entity.HistoryMessage

// internal/entity/model_knowledge.go

// Dataset is a knowledge base.
type Dataset = entity.Dataset

// Document is one document inside a dataset.
type Document = entity.Document

// IndexingStatus is how far the indexing of one batch has got. Uploading a
// document returns before it is searchable; this is what says when it is.
type IndexingStatus = entity.IndexingStatus

// MetadataField is a field documents in one knowledge base may carry.
//
// Two kinds arrive in this shape and the difference is the id: a field you
// defined has one and can be renamed or deleted by it, while one of Dify's
// own — filename, upload date — has none, because those are turned on and
// off rather than managed.
type MetadataField = entity.MetadataField

// PipelineIngestion is what running a published pipeline queued.
//
// A published run does not answer with the work: it enqueues one document
// per source and answers with the batch they share. The documents are not
// indexed yet — Documents.IndexingStatus(batch) is how far it has got, and
// Documents.WaitUntilIndexed waits for it.
type PipelineIngestion = entity.PipelineIngestion

// RetrievalHit is one segment retrieval found, and how well it matched.
type RetrievalHit = entity.RetrievalHit

// Segment is a chunk of a document, as retrieval sees it.
type Segment = entity.Segment

// Tag is a label across the workspace's knowledge bases.
type Tag = entity.Tag

// Model is one model this workspace can call.
type Model = entity.Model

// ModelProvider is a configured provider, and the models it offers.
type ModelProvider = entity.ModelProvider

// internal/entity/usage.go

// Amount is a decimal money figure, kept exact. Dify reports prices as
// decimal strings ("0.000214"); a float64 would add rounding error on every
// sum, and a budget check is exactly where that is not acceptable.
type Amount = entity.Amount

// ParseAmount reads a decimal string such as "0.000214".
func ParseAmount(s string) (Amount, error) { return entity.ParseAmount(s) }

// Usage is what a run consumed, summed across every model call it made.
//
// Costs are kept per currency rather than as one number, because a workflow
// may call providers that price in different ones and adding those together
// would produce a figure that means nothing.
type Usage = entity.Usage

// ErrCostUnknown is returned by TotalPrice when nothing reported a cost.
var ErrCostUnknown = entity.ErrCostUnknown

// internal/codec/paging.go

// MaxWalk is how many items Page.All walks before it stops and says so.
//
// A walk is a loop the server controls: it ends when Dify stops saying
// has_more. A server that never stops — a bug, a proxy, a listing growing
// faster than it is read — would otherwise be an unbounded number of requests
// inside what reads like an ordinary for loop. Reaching this yields an error
// rather than quietly truncating, because a walk that stops early without
// saying so is the bug All exists to fix.
const MaxWalk = codec.MaxWalk

// PageLimitError is yielded by Page.All when a listing keeps going past its
// ceiling. Everything before it was yielded normally.
type PageLimitError = codec.PageLimitError

// Page is one page of a listing, and how to get the rest.
//
// Dify pages some listings by number and others by cursor (last_id or
// first_id), and answers a few with a bare array. The difference stays
// visible in each listing's parameters; what a caller does with the result
// does not depend on it.
//
//	page, err := app.Chat.Conversations.List(ctx, nil)
//	for _, c := range page.Items { ... }           // this page
//	for c, err := range page.All(ctx) { ... }      // every page, fetched as it goes
type Page[T any] = codec.Page[T]

// internal/codec/stream.go

// RunEvent is one event from a stream, with the parts this SDK understands
// pulled out. Payload is always the whole thing Dify sent, so a field this SDK
// has never heard of still reaches the caller.
type RunEvent = codec.RunEvent

// WorkflowRunStream is a workflow run, watched as it happens.
//
//	stream, err := app.Workflows.Runs.Stream(ctx, inputs, nil)
//	if err != nil { ... }
//	defer stream.Close()
//	for event, err := range stream.Events() {
//		if err != nil { ... }
//		if event.Execution != nil { fmt.Println(event.Execution.NodeID) }
//	}
//	run := stream.FinalRun()
type WorkflowRunStream = codec.WorkflowRunStream

// MessageStream is a chat or completion message, watched as it is written.
//
//	stream, err := app.Chat.Messages.Stream(ctx, "Hello", nil)
//	if err != nil { ... }
//	defer stream.Close()
//	for piece, err := range stream.Text() {
//		if err != nil { ... }
//		fmt.Print(piece)
//	}
//	message := stream.FinalMessage()
type MessageStream = codec.MessageStream

// CollectRun reads a whole Dify event stream into one WorkflowRun — the same
// accumulation the streams do, for a caller holding the raw SSE bytes.
func CollectRun(r io.Reader) (*WorkflowRun, error) { return codec.CollectRun(r) }

// internal/infra/secret.go

// Environment variables, the same names the difyctl CLI reads, so a machine
// set up for one is set up for the other.
const (
	// EnvHost names the Dify host, e.g. http://localhost. The Service API base
	// is derived from it as <host>/v1.
	EnvHost = infra.EnvHost
	// EnvAPIBaseURL overrides that derivation, for the unusual case where the
	// Service API does not sit at <host>/v1.
	EnvAPIBaseURL = infra.EnvAPIBaseURL
	// EnvAPIKey is an app's Service-API key, read by NewApp.
	EnvAPIKey = infra.EnvAPIKey
	// EnvDatasetAPIKey is a knowledge (dataset) key, read by NewKnowledge.
	// Falls back to EnvAPIKey, which is where a single-purpose program keeps it.
	EnvDatasetAPIKey = infra.EnvDatasetAPIKey
)

// DefaultBaseURL is Dify Cloud's Service API.
const DefaultBaseURL = infra.DefaultBaseURL

// KeyFunc produces an API key on demand. It is called for every request
// rather than once, which is what lets a key come from a vault and be rotated
// without rebuilding the client.
type KeyFunc = infra.KeyFunc

// MaskSecret renders a key the way it is safe to print: app-****3f2a.
//
// Dify issues keys with a type prefix (app-, dataset-), which is kept so a
// masked key is still identifiable.
func MaskSecret(value string) string { return kernel.MaskSecret(value) }

// internal/infra/transport.go

const (
	// DefaultTimeout bounds a request whose context carries no deadline. A
	// context with a deadline of its own replaces it, which is how one long
	// workflow run gets twenty minutes without every call getting them.
	DefaultTimeout = infra.DefaultTimeout
	// DefaultMaxRetries is how many times a request is repeated after the
	// first attempt, when repeating it is safe.
	DefaultMaxRetries = infra.DefaultMaxRetries
	// DefaultRetryDelay is the first backoff; each retry doubles it.
	DefaultRetryDelay = infra.DefaultRetryDelay
	// MaxRateLimitWait is the longest this client sits out a 429, however long
	// Dify asks for. A server under maintenance can answer Retry-After: 3600,
	// and a call that blocks for an hour is indistinguishable from one that
	// hung — so beyond this the 429 is returned, with RetryAfter set.
	MaxRateLimitWait = infra.MaxRateLimitWait
)

// Option configures a client. The same options serve App and Knowledge.
type Option = infra.Option

// WithAPIKey sets the key. Left out, it is read from the environment.
func WithAPIKey(key string) Option { return infra.WithAPIKey(key) }

// WithAPIKeyFunc fetches the key per request instead of holding it, so a
// rotating or short-lived key works without rebuilding the client and no
// long-lived secret sits on it.
func WithAPIKeyFunc(f KeyFunc) Option { return infra.WithAPIKeyFunc(f) }

// WithBaseURL sets the Service API root, e.g. http://localhost/v1. Left out,
// it is DIFY_API_BASE_URL, then <DIFY_HOST>/v1, then Dify Cloud.
func WithBaseURL(u string) Option { return infra.WithBaseURL(u) }

// WithHTTPClient sends through your own client — for a proxy, a corporate TLS
// bundle, a shared connection pool, or a RoundTripper that never leaves the
// test process.
func WithHTTPClient(h *http.Client) Option { return infra.WithHTTPClient(h) }

// WithTimeout changes DefaultTimeout. Zero means no timeout beyond the
// context's. On a stream it bounds the silence between two events rather than
// the whole stream, since a run that keeps reporting progress is not hung.
func WithTimeout(d time.Duration) Option { return infra.WithTimeout(d) }

// WithMaxRetries changes DefaultMaxRetries. Zero disables retrying.
func WithMaxRetries(n int) Option { return infra.WithMaxRetries(n) }

// WithRetryDelay changes DefaultRetryDelay.
func WithRetryDelay(d time.Duration) Option { return infra.WithRetryDelay(d) }

// WithUser sets the end-user identifier sent when a call does not name one.
// Dify wants one on nearly every app request; setting it here is the usual
// case, passing it per call is for a server acting for many users.
func WithUser(user string) Option { return infra.WithUser(user) }

// WithLogger logs each request and response at debug level, and retries at
// warn. Headers are never logged, so the key does not end up in a log.
func WithLogger(l *slog.Logger) Option { return infra.WithLogger(l) }

// internal/infra/version.go

// Version is what this SDK reports itself as, read from the build info of the
// program it is compiled into rather than written down here, where it would
// drift from the tag. It is "(devel)" when built from a checkout that is the
// main module, since there is no tag to read.
func Version() string { return infra.Version() }

// UserAgent is what this SDK sends. Dify logs the User-Agent of everything
// that talks to it; Go's default says only "Go-http-client/1.1", which tells
// an operator looking at a misbehaving client nothing about which SDK or
// version it was.
func UserAgent() string { return infra.UserAgent() }

// internal/kernel/errors.go

// Sentinels to test an error against with errors.Is. They classify what went
// wrong without committing a caller to a concrete type:
//
//	errors.Is(err, dify.ErrAuthentication)   401 — the key is wrong or revoked
//	errors.Is(err, dify.ErrRateLimited)      429 — see APIError.RetryAfter
//	errors.Is(err, dify.ErrValidation)       422, or arguments refused before sending
//	errors.Is(err, dify.ErrFileUpload)       an upload Dify would not take
//	errors.Is(err, dify.ErrNotFound)         404
//	errors.Is(err, dify.ErrTimeout)          the request ran past its deadline
//	errors.Is(err, dify.ErrNetwork)          the connection failed
//
// errors.As(err, &apiErr) reaches the *APIError itself, which carries the
// server's message, code and version.
var (
	ErrAuthentication = kernel.ErrAuthentication
	ErrRateLimited    = kernel.ErrRateLimited
	ErrValidation     = kernel.ErrValidation
	ErrFileUpload     = kernel.ErrFileUpload
	ErrNotFound       = kernel.ErrNotFound
	ErrTimeout        = kernel.ErrTimeout
	ErrNetwork        = kernel.ErrNetwork
)

// APIError is Dify answering, and the answer being an error.
//
// It carries which Dify answered. Every response Dify sends — errors included
// — has X-Version and X-Env on it, and the first question about a failing call
// is always which server and which version. By the time someone asks, the
// error message is usually all that is left, so the version goes in it.
type APIError = kernel.APIError

// TransportError is a request that never got an answer — the connection
// failed, or it ran past its deadline.
//
// Sent reports whether the request had already gone out. A request that was
// sent and then timed out may have been acted on: a POST /workflows/run that
// timed out may still have billed a run, which is why it was not retried.
type TransportError = kernel.TransportError

// ArgumentError is a call this client refused before spending a request on
// it. It matches ErrValidation, the same as a 422 from Dify, because to the
// caller both mean "these arguments will not do".
type ArgumentError = kernel.ArgumentError

// internal/usecase/management.go

// Management is the workspace, from an account's side: creating, publishing
// and deleting apps, minting their keys, and what the workspace has installed.
//
// It talks to the console API, which authenticates as an account rather than
// as an app — a credential to the whole account, since Dify has nothing
// narrower. That is why it is a client of its own and not more fields on App:
//
//	m, err := dify.LoginManagement(ctx, email, password, dify.WithHost("http://localhost"))
//	d, err := m.Apps.Deploy(ctx, dsl, nil)
//	if err := d.Err(dify.StageRunnable); err != nil { ... }
//	app, err := m.AppClient(d.APIKey, "alice")
//
// A Management is safe for concurrent use. A session from LoginManagement is
// renewed when it expires; one from tokens is not, since a login is what
// hands out the refresh token.
type Management = usecase.Management

// internal/usecase/management_apps.go

// Apps are the workspace's apps, and the steps between a DSL document and a
// run.
//
// Dify keeps three things apart: importing writes a draft, publishing makes a
// version live, and the Service API runs the live version with a key. Import,
// Publish and Keys.Create are those steps; Deploy does all three and reports
// each as its own fact.
type Apps = usecase.Apps

// AppListParams narrow the workspace's apps. Zero values take Dify's defaults.
type AppListParams = usecase.AppListParams

// ImportParams are the optional parts of importing a DSL.
type ImportParams = usecase.ImportParams

// DeployParams choose which of Deploy's steps happen.
type DeployParams = usecase.DeployParams

// DraftRunParams are the optional parts of running a draft.
type DraftRunParams = usecase.DraftRunParams

// AppKeys are an app's Service-API keys.
//
// Dify shows a key once, when it is minted, and an app holds ten. There is no
// reading one back — the listing masks the token — so mint one, keep it, and
// revoke it when done; a leaked key fills the cap and the error never says
// which ones to revoke.
type AppKeys = usecase.AppKeys

// Triggers are the ways a published workflow starts by itself: a schedule, a
// webhook, a plugin event. A trigger node in a draft is only a drawing; Dify
// makes the trigger, and a webhook's URL, when the workflow publishes.
type Triggers = usecase.Triggers

// ManagedApp is one app from the account's side: what management knows of it,
// and a way to call it.
type ManagedApp = usecase.ManagedApp

// internal/usecase/management_workspace.go

// Agents are the workspace's Agents. Dify keeps them on a roster of their own,
// off the app list, and an Agent publishes there rather than as a workflow.
type Agents = usecase.Agents

// Pipelines are the workspace's knowledge pipelines.
//
// A pipeline is not an app: Dify serves it from /rag/pipelines, its DSL is
// kind: rag_pipeline at version 0.1.0, and importing one creates the knowledge
// base it fills. So two ids come back — the pipeline's and the knowledge
// base's — and deleting the knowledge base is what deletes both.
type Pipelines = usecase.Pipelines

// PipelineImportParams are the optional parts of importing a pipeline.
type PipelineImportParams = usecase.PipelineImportParams

// PipelineDeployParams choose which of a pipeline deploy's steps happen.
type PipelineDeployParams = usecase.PipelineDeployParams

// WorkspaceModels are what the workspace can call, and whether the credentials
// for it are in place.
type WorkspaceModels = usecase.WorkspaceModels

// Tools are the tool providers and plugins installed in the workspace.
type Tools = usecase.Tools

// Skills are the workspace's agent skills.
type Skills = usecase.Skills

// SkillImportParams are the optional parts of importing a skill.
type SkillImportParams = usecase.SkillImportParams

// DatasetKeys are the workspace's knowledge-base API keys: what
// dify.NewKnowledge takes. A workspace holds ten, and a listing masks them,
// so mint one when needed and revoke it when done.
type DatasetKeys = usecase.DatasetKeys

// internal/entity/model_management.go

// Stage is a one-word summary of how far a deploy got, for printing and for
// Deployment.Err. It is derived from the separate facts on a Deployment, never
// stored: an earlier design kept one stage and let a failed import followed by
// a successful publish read as runnable.
type Stage = entity.Stage

const (
	// StageUnknown is a request that went out with no answer back. What Dify
	// did is unknown, so neither retrying nor cleaning up is obviously right.
	StageUnknown = entity.StageUnknown
	// StageNotImported is a document Dify refused, or held for confirmation:
	// no draft exists.
	StageNotImported = entity.StageNotImported
	// StageDrafted is a draft on Dify that nothing runs yet. The Service API
	// runs the published version.
	StageDrafted = entity.StageDrafted
	// StagePublished is a live version with no key in hand to call it with.
	StagePublished = entity.StagePublished
	// StageRunnable is published, with a Service-API key in hand.
	StageRunnable = entity.StageRunnable
)

// StageError is a deploy that stopped short of the stage it was asked for.
type StageError = entity.StageError

// Deployment is what an app deploy came to: which steps happened, as separate
// facts, and where it stopped if it stopped early.
//
//	d, err := management.Apps.Deploy(ctx, dsl, nil)
//	if err := d.Err(dify.StageRunnable); err != nil { ... }
//
// A failed step is reported here rather than as the call's error, because a
// half-done deploy is a state to act on — the app may exist, unpublished — and
// an error alone would lose which half was done.
type Deployment = entity.Deployment

// PipelineDeployment is what a knowledge pipeline deploy came to.
//
// A pipeline is not an app, and it carries two ids: PipelineID addresses the
// graph and DatasetID the knowledge base it fills. Deleting the knowledge base
// is what deletes the pipeline, which is why cleanup needs the second.
type PipelineDeployment = entity.PipelineDeployment

// AppSummary is an app as the workspace lists it.
type AppSummary = entity.AppSummary

// APIKey is a Service-API key, for an app or for the workspace's knowledge
// bases.
//
// Dify shows the whole token when the key is minted. A listing of the
// workspace's dataset keys shows it masked — "datas...3f2a" — and a masked
// token authenticates as "Access token is invalid". (Dify 1.17 lists an app's
// keys in full.)
type APIKey = entity.APIKey

// Trigger is a way a published workflow starts by itself. A trigger node in a
// draft is only a drawing: Dify makes the trigger when the workflow publishes.
type Trigger = entity.Trigger

// WebhookTrigger is the endpoint Dify minted for a webhook trigger node.
type WebhookTrigger = entity.WebhookTrigger

// AgentSummary is an Agent as the workspace's roster reports it. Agents are
// kept off the app list.
//
// ID addresses the Agent on the roster, which is where it publishes; AppID is
// what every app-shaped call wants — export, delete, keys.
type AgentSummary = entity.AgentSummary

// PipelineSummary is a knowledge pipeline as the workspace reports it: read off
// the knowledge base that owns it, since a pipeline has no listing of its own.
type PipelineSummary = entity.PipelineSummary

// WorkspaceSkill is an agent skill installed in the workspace.
type WorkspaceSkill = entity.WorkspaceSkill

// ToolProvider is a tool provider installed in the workspace, with its tools.
type ToolProvider = entity.ToolProvider

// Tool is one tool a provider offers.
type Tool = entity.Tool

// ToolParameter is one parameter a tool takes. Form is "llm" when the model
// fills it at run time, and "form" when the workflow sets it.
type ToolParameter = entity.ToolParameter

// Plugin is a plugin installed in the workspace.
type Plugin = entity.Plugin

// internal/infra/console.go

const (
	// EnvConsoleToken is a console session's access token, read by
	// NewManagement. Named apart from difyctl's DIFY_TOKEN, which holds an
	// /openapi/v1 bearer — a different credential the console refuses.
	EnvConsoleToken = infra.EnvConsoleToken
	// EnvConsoleCSRFToken is the CSRF token that goes with it. Since Dify 1.17
	// every console request, reads included, needs both.
	EnvConsoleCSRFToken = infra.EnvConsoleCSRFToken
	// DefaultConsoleHost is Dify Cloud.
	DefaultConsoleHost = infra.DefaultConsoleHost
)

// WithHost sets the Dify host, e.g. http://localhost. NewManagement talks to
// <host>/console/api; NewApp and NewKnowledge derive <host>/v1 from it unless
// WithBaseURL says otherwise. Left out, it is DIFY_HOST, then Dify Cloud.
func WithHost(host string) Option { return infra.WithHost(host) }

// WithConsoleToken sets a console session's access token. Left out, it is read
// from DIFY_CONSOLE_TOKEN. dify.LoginManagement obtains one from an email and
// password instead.
func WithConsoleToken(token string) Option { return infra.WithConsoleToken(token) }

// WithCSRFToken sets the CSRF token that goes with a console access token.
// Left out, it is read from DIFY_CONSOLE_CSRF_TOKEN. A Dify before 1.17 needs
// none; since 1.17 every request is refused without it.
func WithCSRFToken(token string) Option { return infra.WithCSRFToken(token) }

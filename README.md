# dify-go-sdk

[![Go Reference](https://pkg.go.dev/badge/github.com/langgenius/dify-go-sdk.svg)](https://pkg.go.dev/github.com/langgenius/dify-go-sdk)
[![CI](https://github.com/langgenius/dify-go-sdk/actions/workflows/ci.yml/badge.svg)](https://github.com/langgenius/dify-go-sdk/actions/workflows/ci.yml)

A Go client for [Dify](https://github.com/langgenius/dify) — running apps and
managing knowledge bases through the Service API, and deploying apps through
the console API. A port of the "using Dify" half of
[dify-python-sdk](https://github.com/langgenius/dify-python-sdk), verified
against Dify 1.17.1. Go 1.24+, no dependencies beyond the standard library.

The API may still change between 0.x releases; [CHANGELOG.md](CHANGELOG.md)
says what did.

```bash
go get github.com/langgenius/dify-go-sdk
```

## Start here

```go
app, err := dify.NewApp(dify.WithAPIKey("app-…"), dify.WithUser("alice"))

msg, err := app.Chat.Messages.Create(ctx, "Hello", nil)
fmt.Println(msg.Answer, msg.ConversationID)

run, err := app.Workflows.Runs.Create(ctx, map[string]any{"text": "…"}, nil)
fmt.Println(run.Status, run.Outputs, run.Usage())
```

A client holds the connection and the credential. What you can do hangs off
what it acts on:

| | |
|---|---|
| `app.Chat.Messages` | `Create`, `Stream`, `List`, `Stop`, `Feedback`, `Suggested` |
| `app.Chat.Conversations` | `List`, `Rename`, `Delete`, `Variables`, `SetVariable` |
| `app.Workflows.Runs` | `Create`, `Stream`, `Retrieve`, `Events`, `Stop`, `Logs` |
| `app.Completions` | `Create`, `Stream`, `Stop` |
| `app.Files` | `Upload`, `Download`, `PreviewURL` |
| `app.Annotations` | `List`, `Create`, `Update`, `Delete`, `SetReply`, `ReplyStatus` |
| `app.Audio` | `Speak`, `Transcribe` |
| `app.Forms` | `Retrieve`, `Submit` |
| `app` | `Info`, `Parameters`, `Site`, `Meta`, `Feedbacks`, `EndUser`, `ServerInfo` |

Calls return the thing, carrying the ids the next call needs — `run.RunID`
reads a run back, `run.TaskID` is what a stop names, and they are not the same
id. Required arguments are positional; optional ones go in a `*…Params`
struct, where `nil` is fine. Every typed result keeps Dify's whole answer on
`Raw`, so a field a newer Dify adds is never lost.

### Is this host a Dify?

```go
info, err := dify.Probe(ctx, "http://localhost/v1")  // no credential
// Dify 1.17.1 (v1)
```

## Streams

```go
stream, err := app.Workflows.Runs.Stream(ctx, inputs, nil)
if err != nil { … }
defer stream.Close()

for event, err := range stream.Events() {
	if err != nil { … }
	if event.Execution != nil {
		fmt.Println(event.Execution.NodeID, event.Execution.Usage)
	}
}
run := stream.FinalRun()
```

For a chat, `stream.Text()` yields just the answer, and `FinalMessage()` is
the message. Things Dify keeps apart, and so does this:

- **Closing the stream** stops watching. The run keeps going — stop it with
  `Runs.Stop(ctx, run.TaskID, "")`.
- **A pause** is the run waiting for a person. `run.Paused()` is true while
  `Succeeded()` and `Failed()` are both false.
- **A dropped connection** says nothing about the run. Reopen it with
  `Runs.Events(ctx, run.RunID, nil)`.
- **An HTTP 200** is not a successful run. Dify reports a mid-stream failure
  as an `error` event after the status line; the stream yields it as an
  `*APIError` with `InStream` set.
- **A stream that stopped** is not a finished message. `msg.Finished` is set
  only when Dify says so.

### Waiting for a person

```go
if run.Paused() {
	token, err := dify.FormToken(run)   // explains a pause with no token
	form, _ := app.Forms.Retrieve(ctx, token)
	_ = app.Forms.Submit(ctx, token, map[string]any{"decision": "approve"}, "approve", "")
	resumed, _ := app.Workflows.Runs.Events(ctx, run.RunID, nil)
	for range resumed.Events() {}
	run = resumed.FinalRun()
}
```

## Knowledge

```go
k, err := dify.NewKnowledge(dify.WithAPIKey("dataset-…"))

retrieval, err := dify.RetrievalModel(&dify.RetrievalModelParams{Search: dify.SearchHybrid, TopK: 5})
base, err := k.Datasets.Create(ctx, "handbook", &dify.DatasetCreateParams{
	IndexingTechnique: "high_quality",
	Retrieval:         retrieval,
})

docs := k.Documents(base.ID)
doc, err := docs.CreateFromText(ctx, "policy", "Refunds take five days.", nil)
_, err = docs.WaitUntilIndexed(ctx, doc.Batch, nil)   // not searchable before this

hits, err := k.Datasets.Search(ctx, base.ID, "what is the refund window?", nil)
```

`k.Tags`, `k.Pipeline(datasetID)`, `k.Models(ctx, "text-embedding")` and
`docs.Segments(documentID)` cover the rest. How a base is searched is stored
on the base, so `RetrievalModel` decides every later retrieval; it refuses a
reranking model and weights together, and a score threshold sets both of the
fields Dify needs. A published pipeline run (`Pipeline.Run`) is queued and
answers with a batch of documents; a draft run (`RunDraft`) executes the graph
and answers with a `WorkflowRun`.

## Management

Creating, publishing and deleting apps is the console API's, and it
authenticates as an account rather than as an app:

```go
m, err := dify.LoginManagement(ctx, email, password, dify.WithHost("http://localhost"))

d, err := m.Apps.Deploy(ctx, dsl, nil)            // import, publish, mint a key
if err := d.Err(dify.StageRunnable); err != nil {  // where it stopped, and what is left
	return err
}
app, err := m.AppClient(d.APIKey, "alice")        // an App on the same Dify
```

A deploy reports each step as its own fact — `Imported`, `Published`,
`APIKey` — plus `NeedsConfirmation` for an import Dify holds over a DSL
version difference, and `Indeterminate` for one whose answer never arrived,
since that app may exist. `Apps.Import`, `Publish`, `Confirm` and `RunDraft`
(run the draft without publishing it) are the steps on their own;
`Apps.Temporary` deploys an app for a test and cleans up if it stops short.

`m.Apps.Keys`, `m.Apps.Triggers`, `m.Agents`, `m.Pipelines`, `m.Models`,
`m.Tools`, `m.Skills` and `m.DatasetKeys` cover the rest.

A session from `LoginManagement` is renewed when it expires, and is the
credential to the whole account for as long as its refresh token lasts —
30 days by default — so end it with `m.Logout(ctx)` when done.
`m.SessionTokens()` hands the current tokens to another process; `NewManagement`
takes them back as `DIFY_CONSOLE_TOKEN` and, since Dify 1.17,
`DIFY_CONSOLE_CSRF_TOKEN`, which every console request needs, reads included.

## Listings

Every listing returns a `*Page[T]`, whether Dify pages it by number, by
cursor, or not at all:

```go
page, err := app.Chat.Conversations.List(ctx, nil)
for _, c := range page.Items { … }            // this page
for c, err := range page.All(ctx) { … }       // every page, fetched as it goes
all, err := page.Collect(ctx)                 // or all of them at once
```

`All` walks a loop the server controls, so it stops at `dify.MaxWalk` (10,000)
items with a `*PageLimitError` rather than fetching forever; pass a ceiling to
raise it. A cursor that comes back unchanged ends the walk too.

## Errors

```go
var apiErr *dify.APIError
switch {
case errors.Is(err, dify.ErrAuthentication):   // 401
case errors.Is(err, dify.ErrRateLimited):      // 429 — apiErr.RetryAfter
case errors.Is(err, dify.ErrValidation):       // 422, or refused before sending
case errors.Is(err, dify.ErrNotFound):         // 404
case errors.Is(err, dify.ErrTimeout), errors.Is(err, dify.ErrNetwork):
case errors.As(err, &apiErr):
	fmt.Println(apiErr.Message, apiErr.Code, apiErr.ServerVersion)
}
```

An error Dify answered carries the server it came from:
`dify: Access token is invalid (unauthorized) [Dify 1.17.1]`.

## Retries and timeouts

A request that failed before it was sent is always retried. One that was sent
is retried only for an idempotent method — repeating a timed-out
`POST /workflows/run` can bill the same run again. A 429 was refused, so it is
waited out for any method, honouring `Retry-After`, unless the server asks for
more than a minute.

Each request is bounded by `WithTimeout` (default 60s) unless its context
carries a deadline, which replaces it:

```go
ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)   // one long run
defer cancel()
run, err := app.Workflows.Runs.Create(ctx, inputs, nil)
```

On a stream the timeout bounds the silence between events, not the length of
the stream.

## Configuration

| Variable | What it is |
|---|---|
| `DIFY_HOST` | the Dify host, e.g. `http://localhost`; the Service API is `<host>/v1` |
| `DIFY_API_BASE_URL` | overrides that derivation |
| `DIFY_API_KEY` | an app's key, read by `NewApp` |
| `DIFY_DATASET_API_KEY` | a knowledge key, read by `NewKnowledge` (falls back to `DIFY_API_KEY`) |
| `DIFY_CONSOLE_TOKEN` | a console access token, read by `NewManagement` |
| `DIFY_CONSOLE_CSRF_TOKEN` | the CSRF token that goes with it |

Options win over the environment. An explicitly empty `WithAPIKey("")` is an
error, not a fallback. `WithAPIKeyFunc` fetches the key per request, for a
vault or a rotating key. The key is masked wherever the client is printed.
`WithHTTPClient` sends through your own `*http.Client`; `WithLogger` logs at
debug level and never logs headers.

## Where this differs from the Python SDK

Each checked against Dify's controllers:

- `Annotations.SetReply` requires the embedding provider, model and threshold
  on disable too; Dify rejects an empty disable.
- `MessageParams` exposes `WorkflowID` (pin a chatflow version) and
  `NoAutoName`.
- `Documents.DownloadURL` returns the signed URL Dify answers with; the Python
  `download()` treats that JSON as the file's bytes.
- Updating a document by text takes no embedding fields; only the file update
  does, as in Dify's payload models.
- The console session sends its CSRF token on reads too, finds its cookies
  under the `__Host-` names Dify uses behind https, and renews itself with the
  refresh token a login hands out.
- A skill is deleted with its display name as confirmation, the field Dify
  checks, and only when something references it.

Not ported: workflow and agent definitions (built on the Python-only `graphon`
engine) — Management deploys DSL text — and `OpenApiClient`.

## Development

```bash
go test -race ./...                         # offline
export DIFY_HOST=… DIFY_CONSOLE_EMAIL=… DIFY_CONSOLE_PASSWORD=…
go test -run Live -v ./tests                # against a running Dify
```

The live harness creates its apps and a dataset key through the console,
runs template-only workflows (no model cost), and deletes everything
afterwards. See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md).

## Security

Report vulnerabilities privately, as [SECURITY.md](SECURITY.md) describes —
not in a public issue.

## License

[MIT](LICENSE)

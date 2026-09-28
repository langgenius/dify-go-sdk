// Package dify is a Go client for Dify: its Service API, and the console API
// for managing a workspace.
//
// Three clients, because Dify scopes three credentials:
//
//	App         an app's key (app-…)      running one app, its conversations, its files
//	Knowledge   a dataset key (dataset-…) knowledge bases, documents, retrieval
//	Management  a console session         deploying apps, keys, what the workspace has
//
// A client holds the connection and the credential. What you can do hangs off
// it by what it acts on — app.Chat.Messages, app.Workflows.Runs,
// knowledge.Datasets — and a call returns the thing, not an HTTP response,
// carrying the ids the next call needs:
//
//	app, err := dify.NewApp(dify.WithAPIKey("app-…"), dify.WithUser("alice"))
//	run, err := app.Workflows.Runs.Create(ctx, map[string]any{"text": "…"}, nil)
//	run.RunID    // reads the run back later
//	run.TaskID   // what a stop request names — a different id
//	run.Outputs  // what the workflow produced
//
// # Configuration
//
// Keys and hosts resolve from options first, then the environment, using the
// names the difyctl CLI reads: DIFY_API_KEY, DIFY_HOST (the Service API is
// derived as <host>/v1), and DIFY_API_BASE_URL to override that derivation.
// Knowledge reads DIFY_DATASET_API_KEY, then DIFY_API_KEY. Management reads
// DIFY_CONSOLE_TOKEN and DIFY_CONSOLE_CSRF_TOKEN, or logs in with
// LoginManagement, and talks to <host>/console/api.
//
// # Streams
//
// A stream is how a run is observed. Events yields each event as a Go 1.23
// iterator; FinalRun and FinalMessage answer what it came to afterwards.
// Closing a stream stops watching, not the run. A pause is not a failure:
// WorkflowRun.Paused, Succeeded and Failed are three separate questions. An
// HTTP 200 is not a successful run: Dify reports a mid-stream failure as an
// "error" event, which the stream yields as an *APIError.
//
// # Errors
//
// Test errors with errors.Is against ErrAuthentication, ErrRateLimited,
// ErrValidation, ErrNotFound, ErrFileUpload, ErrTimeout and ErrNetwork, and
// reach Dify's message, code and version with errors.As(err, &apiErr).
//
// # Retries and timeouts
//
// A request that failed before it was sent is always retried. One that was
// sent is retried only for an idempotent method: repeating a timed-out POST
// /workflows/run can bill the run again. A 429 is waited out for any method,
// honouring Retry-After up to MaxRateLimitWait.
//
// Each request is bounded by DefaultTimeout unless its context carries a
// deadline, which replaces it — give one long workflow run twenty minutes
// with context.WithTimeout rather than raising the timeout for every call. On
// a stream the timeout bounds the silence between events, not its length.
package dify

# Working on dify-go-sdk

Instructions for coding agents. `CLAUDE.md` points here; keep this the only copy.

This is a Go port of the Service-API half of
[dify-python-sdk](https://github.com/langgenius/dify-python-sdk) (`DifyApp`,
`DifyKnowledge`). That repo's `AGENTS.md` records why most shapes here are the
way they are — every row of its "Distinctions to preserve" table was a bug
once, and the port keeps them. Read it before changing behaviour.

Not ported: workflow/agent definitions (they build on the Python-only
`graphon` engine), `DifyManagement` (console API), `OpenApiClient`.

## Verify against a running Dify

Dify's controllers are the specification, not its docs and not the Python SDK:
`../dify-oss/api/controllers/service_api/`. Read a route's pydantic query or
payload model before adding or changing a call. Two places the port already
departs from the Python SDK because the controller said so:

- `Annotations.SetReply` takes the embedding provider, model and threshold on
  **disable** too — `AnnotationReplyActionPayload` requires them for both, and
  `TestLiveDisablingAnnotationReplyStillNeedsTheEmbeddingFields` pins it.
- `MessageParams` carries `WorkflowID` and `NoAutoName`, which
  `ChatRequestPayload` accepts and the Python SDK does not expose.

When a claim cannot be checked against a real server, say so rather than
implying it was. A mocked server proves the request shape, nothing more.

## Commands

```bash
go test -race ./...                       # offline; the live harness skips itself
gofmt -l . && go vet ./...                # both must be clean

set -a; . ../dify-python-sdk/.env; set +a # DIFY_HOST + console email/password
go test -run Live -v ./...                # against the local Dify
```

The live harness (`live_test.go`) logs in to the console, imports the fixture
apps in `testdata/` (template nodes only, so runs cost nothing), publishes
them, mints keys and a dataset key, and deletes everything at the end —
including leftovers from a crashed run, found by the `sdk-go-harness` prefix.
To regenerate a fixture, build it with the Python SDK's `tests/live/conftest.py`
helpers and `wf.to_yaml()`.

## Shape of the package

One package, `dify`. A client holds the transport; resources hold verbs for
one noun (`app.Chat.Messages`, `app.Workflows.Runs`, `knowledge.Datasets`).
Required arguments are positional, optional ones go in a `*XxxParams` struct
where nil is allowed. Every typed result keeps the whole answer on `Raw`.

| | |
|---|---|
| `transport.go` | options, retries, 429 handling, error mapping, stream idle timeout |
| `shape.go` | lenient reads of Dify's JSON (`object`), because its types drift |
| `paging.go` | `Page[T]`, `All`/`Collect`, and the page-number vs cursor builders |
| `stream.go` | SSE decoding, the `watcher` that accumulates a run, the two streams |
| `results.go`, `usage.go` | what calls return; `Amount` keeps prices exact |
| `app.go` and one file per resource | the App client |
| `knowledge*.go` | the Knowledge client |

## Rules

- **Retrying is not always safe.** A request that was written and then timed
  out is retried only for idempotent methods. `transport.send` learns whether
  it was written from `httptrace.WroteRequest`; keep that.
- **A listing must terminate.** New listings build their page with
  `fetchByPage` / `fetchByCursor` / `unpaged`, never by hand; pick `newest` or
  `oldest` for which end continues a cursor.
- **Never collapse two states into one**: paused vs failed, cost unknown
  (`Costs == nil`) vs free (empty map), a stream closed vs a run stopped, a
  finished message vs one cut short.
- **Comments say why**, never what. Test names are sentences describing the
  behaviour; say in the test when it guards something that was once broken.
- **Errors name the fix.** `argError` for anything refused before sending.
- No third-party dependencies.

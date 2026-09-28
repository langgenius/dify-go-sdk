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

One package, `dify`, so callers write `dify.X`. Resources hold verbs for one
noun (`app.Chat.Messages`, `app.Workflows.Runs`, `knowledge.Datasets`).
Required arguments are positional, optional ones go in a `*XxxParams` struct
where nil is allowed. Every typed result keeps the whole answer on `Raw`.

Inside, the files are layered, and a layer uses only its own and the inner
ones. With one package the compiler cannot hold that line, so
`architecture_test.go` does: it type-checks the package and fails on a
crossing, on a resource reading Dify's JSON itself, and on a file in no layer.

| Layer | Files | May use |
|---|---|---|
| kernel | `shape.go` (lenient reads of Dify's JSON, `object`), `errors.go`, `clock.go` | kernel |
| entity | `model_app.go`, `model_knowledge.go`, `usage.go` (`Amount` keeps prices exact) | kernel |
| codec | `decode_app.go`, `decode_knowledge.go`; `stream.go` (SSE, the `watcher` that accumulates a run); `paging.go` (`Page[T]`, page-number vs cursor builders) | kernel, entity |
| port | `port.go`: the `port` interface, `request`, `params` | kernel, entity |
| use case | `app.go`, `knowledge.go` and one file per resource: verbs, params, which request each sends | all of the above |
| infra | `transport.go` (options, retries, 429, error mapping, stream idle timeout), `secret.go`, `version.go` | kernel, entity, codec, port |
| root | `client.go`: `NewApp`, `OpenApp`, `NewKnowledge`, `Probe` | everything |

A resource talks to Dify only through `port`, and hands what comes back to a
`xxxFrom` decoder or returns it whole with `raw()`: a field read, or a
fallback between two spellings of one, goes in a `decode_*.go` file. A new
file goes into `layers` in `architecture_test.go` and into this table.

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

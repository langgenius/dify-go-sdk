# Working on dify-go-sdk

Instructions for coding agents. `CLAUDE.md` points here; keep this the only copy.

This is a Go port of the "using Dify" half of
[dify-python-sdk](https://github.com/langgenius/dify-python-sdk) (`DifyApp`,
`DifyKnowledge`, `DifyManagement`). That repo's `AGENTS.md` records why most shapes here are the
way they are — every row of its "Distinctions to preserve" table was a bug
once, and the port keeps them. Read it before changing behaviour.

Not ported: workflow/agent definitions (they build on the Python-only
`graphon` engine), so Management deploys DSL text; `OpenApiClient` and the
console's `mint_openapi_token`.

## Verify against a running Dify

Dify's controllers are the specification, not its docs and not the Python SDK:
`../dify-oss/api/controllers/service_api/` for App and Knowledge,
`../dify-oss/api/controllers/console/` for Management. Read a route's pydantic query or
payload model before adding or changing a call. Two places the port already
departs from the Python SDK because the controller said so:

- `Annotations.SetReply` takes the embedding provider, model and threshold on
  **disable** too — `AnnotationReplyActionPayload` requires them for both, and
  `TestLiveDisablingAnnotationReplyStillNeedsTheEmbeddingFields` pins it.
- `MessageParams` carries `WorkflowID` and `NoAutoName`, which
  `ChatRequestPayload` accepts and the Python SDK does not expose.

When a claim cannot be checked against a real server, say so rather than
implying it was. A mocked server proves the request shape, nothing more.

The console departs from the Python SDK in more places, each read off the
1.17.1 controllers:

- **The CSRF token is checked on every method but OPTIONS**, reads included
  (`libs/login.py`), not on writes only; the bearer header does not exempt a
  request. Only `/apps/<id>/workflows/draft…` is whitelisted.
- **Cookie names get a `__Host-` prefix** when both console URLs are https
  and no `COOKIE_DOMAIN` is set (`libs/token.py`), and Dify reads only the name
  it would write. A login sends back the names it was given; a token from the
  environment goes under both spellings.
- **A login hands out a refresh token**, as a cookie only; `POST
  /refresh-token` renews all three. A session from `LoginManagement` is renewed
  once on a 401 and the request sent again — safe for any method, since Dify
  refused it.
- **A workflow publish reports no version id**, only `created_at`, so
  `Deployment.Version` is set for an Agent (its snapshot id) and nothing else.
- **A skill's delete confirmation is its `display_name`**, and is needed only
  when something references the skill; publish and delete read a JSON body
  and refuse a missing one.
- **Dify 1.17 lists an app's keys in full.** The workspace's dataset keys are
  masked as `token[:5]...token[-4:]`.
- **A failed import answers 400 with the import's status in the body**, and a
  held one 202. Both are states on the `Deployment`, not errors.

## Commands

```bash
go test -race ./...                       # offline; the live harness skips itself
gofmt -l . && go vet ./...                # both must be clean

set -a; . ../dify-python-sdk/.env; set +a # DIFY_HOST + console email/password
go test -run Live -v ./...                # against the local Dify
```

The live harness (`tests/live_test.go`) logs in with `dify.LoginManagement`,
deploys the fixture apps in `tests/testdata/` with `Management.Apps.Deploy`
(template nodes only, so runs cost nothing), mints a dataset key with
`Management.DatasetKeys`, and deletes everything at the end —
including leftovers from a crashed run, found by the `sdk-go-harness` prefix.
To regenerate a fixture, build it with the Python SDK's `tests/live/conftest.py`
helpers and `wf.to_yaml()`.

## Shape of the package

Callers write `dify.X` and import one package. Resources hold verbs for one
noun (`app.Chat.Messages`, `app.Workflows.Runs`, `knowledge.Datasets`).
Required arguments are positional, optional ones go in a `*XxxParams` struct
where nil is allowed. Every typed result keeps the whole answer on `Raw`.

The code lives in layers under `internal/`, one package each, so nothing but
the root is importable from outside. The root re-declares what callers use:

| Where | What | Imports |
|---|---|---|
| `client.go` | `NewApp`, `OpenApp`, `NewKnowledge`, `Probe`: the one place infra is wired to the port | everything |
| `dify.go` | an alias or a forwarding func for every name callers use | everything |
| `internal/usecase` | the resources: verbs, params, which request each sends | kernel, entity, codec, port |
| `internal/infra` | `Transport` (options, retries, 429, error mapping, stream idle timeout), credentials, version | kernel, entity, codec, port |
| `internal/port` | `Port`, the interface a resource sends through; `Request`, `Params` | kernel, entity |
| `internal/codec` | `*From` decoders; SSE and the `watcher` that accumulates a run; `Page[T]` and its builders | kernel, entity |
| `internal/entity` | what calls return; `Usage` and `Amount`, which keeps prices exact | kernel |
| `internal/kernel` | `Object` (lenient reads of Dify's JSON, because its types drift), errors | nothing |

`tests/architecture_test.go` holds the "Imports" column, and fails when a
resource reads Dify's JSON itself: it hands the answer to a `codec` decoder
or returns it whole with `Raw()`, so a field read, or a fallback between two
spellings of one, goes in `internal/codec`.

A new exported name in an internal package is not public until `dify.go`
re-declares it. Add it there with its doc comment. A name only another layer
needs stays out of `dify.go`.

Tests that drive the SDK through `dify.X` against a fake or a live Dify are in
`tests/`; a test of one layer's internals sits beside it
(`internal/entity/usage_test.go`). Examples stay in the root, where `go doc`
finds them.

## Rules

- **Retrying is not always safe.** A request that was written and then timed
  out is retried only for idempotent methods. `infra.Transport.send` learns whether
  it was written from `httptrace.WroteRequest`; keep that.
- **A listing must terminate.** New listings build their page with
  `codec.FetchByPage` / `FetchByCursor` / `Unpaged`, never by hand; pick
  `Newest` or `Oldest` for which end continues a cursor.
- **Never collapse two states into one**: paused vs failed, cost unknown
  (`Costs == nil`) vs free (empty map), a stream closed vs a run stopped, a
  finished message vs one cut short.
- **Comments say why**, never what. Test names are sentences describing the
  behaviour; say in the test when it guards something that was once broken.
- **Errors name the fix.** `kernel.ArgError` for anything refused before sending.
- No third-party dependencies.

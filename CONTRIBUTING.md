# Contributing to dify-go-sdk

Thanks for helping. Before changing anything, read [AGENTS.md](AGENTS.md): it
records how the package is laid out, the rules the code keeps, and the Dify
behaviours that each cost debugging time once. It is written for coding agents
and holds for people just as well.

## The short version

- **Dify's controllers are the specification** — not its docs, and not the
  Python SDK. Read the route's pydantic query or payload model in
  [langgenius/dify](https://github.com/langgenius/dify) under
  `api/controllers/` before adding or changing a call, and say in the PR which
  one you read.
- **A mocked server proves the request shape, nothing more.** A change to what
  a call sends or reads should be run against a real Dify with the live
  harness, and the PR should say whether it was.
- **Test names are sentences** describing the behaviour. When a test guards
  something that was once broken, say so in it.
- **Comments say why, never what.**
- **No third-party dependencies.**

## Commands

```bash
go test -race ./...                        # offline; the live harness skips itself
gofmt -l . && go vet ./...                 # both must be clean

# against a running Dify (the harness creates and deletes everything it uses)
export DIFY_HOST=http://localhost DIFY_CONSOLE_EMAIL=… DIFY_CONSOLE_PASSWORD=…
go test -run Live -v ./tests
```

The live harness costs nothing to run: its fixture apps use template nodes,
not model nodes.

## Pull requests

Keep one change per pull request, with a test for it. Describe what you
checked against a real Dify and what you could not. Record user-visible
changes in [CHANGELOG.md](CHANGELOG.md) under "Unreleased".

All discussion, issues and pull requests are in English, following Dify's
[Code of Conduct](CODE_OF_CONDUCT.md).

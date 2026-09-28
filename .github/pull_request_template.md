## Summary

<!-- What changes, and why. Link the issue: `Fixes #123`. -->

## Checked against

<!-- Which Dify controller or payload model this follows, and whether the live
harness was run against a real Dify (`go test -run Live -v ./tests`). A mocked
server proves the request shape, nothing more. -->

## Checklist

- [ ] `gofmt -l .` and `go vet ./...` are clean, and `go test -race ./...` passes
- [ ] Each change has a test, named as a sentence
- [ ] User-visible changes are in CHANGELOG.md under "Unreleased"

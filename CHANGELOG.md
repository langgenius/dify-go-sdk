# Changelog

All notable changes to this project are recorded here. The project follows
[Semantic Versioning](https://semver.org/); while it is below 1.0, a minor
version may change the API.

## Unreleased

## v0.0.1 — 2026-09-28

The first release: a Go client for Dify, ported from the "using Dify" half of
[dify-python-sdk](https://github.com/langgenius/dify-python-sdk) and checked
against Dify 1.17.1.

### Added

- `App`, for one app's Service API: chat messages and conversations, workflow
  runs and their events, completions, files, annotations, audio and
  human-input forms. Streams are Go 1.23 iterators that accumulate a
  `WorkflowRun` or `Message`.
- `Knowledge`, for the workspace's knowledge bases: datasets, documents,
  segments, metadata, tags, retrieval settings, and RAG pipeline runs.
- `Management`, for the console API as an account: deploying apps (import,
  publish, key) with each step reported on a `Deployment`, app keys and
  triggers, Agents, knowledge pipelines, models, tools and plugins, skills,
  and dataset keys. A login's session renews itself and ends with `Logout`.
- Retries that repeat a request only when that is safe, 429 handling that
  honours Retry-After, stream idle timeouts, and errors classified by
  `errors.Is` with the Dify version that answered.

Requires Go 1.24 or later. No dependencies beyond the standard library.

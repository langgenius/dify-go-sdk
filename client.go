package dify

import (
	"context"
)

// The constructors are the one place that knows a *transport stands behind
// every port. Everything they build is handed the port and nothing more, so
// swapping what carries a call is a change here and nowhere else.

// NewApp builds a client for one app. The key comes from WithAPIKey, or
// DIFY_API_KEY; the base URL from WithBaseURL, DIFY_API_BASE_URL, or
// <DIFY_HOST>/v1. It sends nothing.
func NewApp(opts ...Option) (*App, error) {
	t, err := newTransport(opts, EnvAPIKey)
	if err != nil {
		return nil, err
	}
	return newAppOn(t), nil
}

// OpenApp builds a client and asks Dify what the app is before returning it.
// Costs one request; worth it when the key comes from configuration and a
// wrong one should fail here rather than on the first run.
func OpenApp(ctx context.Context, opts ...Option) (*App, *AppInfo, error) {
	app, err := NewApp(opts...)
	if err != nil {
		return nil, nil, err
	}
	info, err := app.Info(ctx)
	if err != nil {
		return nil, nil, err
	}
	return app, info, nil
}

// Probe asks a Dify what version it is, with no client and no credential —
// for finding out whether a host is a Dify at all, since an unreachable host
// and a wrong key look the same once authenticated calls start. baseURL is
// the Service API root; empty resolves it the way NewApp does.
func Probe(ctx context.Context, baseURL string) (*ServerInfo, error) {
	return fetchServerInfo(ctx, newProbeTransport(baseURL))
}

// NewKnowledge builds a client for the workspace's knowledge bases. The key
// comes from WithAPIKey, then DIFY_DATASET_API_KEY, then DIFY_API_KEY — the
// fallback is where a single-purpose program keeps a dataset key it never
// distinguishes from an app key. It sends nothing.
func NewKnowledge(opts ...Option) (*Knowledge, error) {
	t, err := newTransport(opts, EnvDatasetAPIKey, EnvAPIKey)
	if err != nil {
		return nil, err
	}
	return newKnowledgeOn(t), nil
}

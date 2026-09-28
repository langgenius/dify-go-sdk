package dify

import (
	"context"

	"github.com/langgenius/dify-go-sdk/internal/infra"
	"github.com/langgenius/dify-go-sdk/internal/usecase"
)

// The constructors are the one place that knows an infra.Transport stands
// behind every port. Everything they build is handed the port and nothing more, so
// swapping what carries a call is a change here and nowhere else.

// NewApp builds a client for one app. The key comes from WithAPIKey, or
// DIFY_API_KEY; the base URL from WithBaseURL, DIFY_API_BASE_URL, or
// <DIFY_HOST>/v1. It sends nothing.
func NewApp(opts ...Option) (*App, error) {
	t, err := infra.NewTransport(opts, EnvAPIKey)
	if err != nil {
		return nil, err
	}
	return usecase.NewAppOn(t), nil
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
	return usecase.FetchServerInfo(ctx, infra.NewProbeTransport(baseURL))
}

// NewKnowledge builds a client for the workspace's knowledge bases. The key
// comes from WithAPIKey, then DIFY_DATASET_API_KEY, then DIFY_API_KEY — the
// fallback is where a single-purpose program keeps a dataset key it never
// distinguishes from an app key. It sends nothing.
func NewKnowledge(opts ...Option) (*Knowledge, error) {
	t, err := infra.NewTransport(opts, EnvDatasetAPIKey, EnvAPIKey)
	if err != nil {
		return nil, err
	}
	return usecase.NewKnowledgeOn(t), nil
}

// NewManagement builds a client for the workspace, from a console session
// given as tokens: WithConsoleToken and WithCSRFToken, or DIFY_CONSOLE_TOKEN
// and DIFY_CONSOLE_CSRF_TOKEN. The host comes from WithHost, DIFY_HOST, or Dify
// Cloud. It sends nothing.
//
// A session given as tokens is not renewed when it expires, since only a
// login hands out the refresh token; LoginManagement is the one that lasts.
func NewManagement(opts ...Option) (*Management, error) {
	t, err := infra.NewConsoleTransport(opts)
	if err != nil {
		return nil, err
	}
	return managementOn(t), nil
}

// LoginManagement logs in to the console with an account's email and
// password, and returns a client holding the session. The session is renewed
// when it expires, for as long as Dify's refresh token lasts (30 days by
// default).
//
// Prefer an account made for automation: this is the credential to the whole
// account, and Dify offers nothing narrower. The password is used for this one
// request and not kept.
func LoginManagement(ctx context.Context, email, password string, opts ...Option) (*Management, error) {
	t, err := infra.ConsoleLogin(ctx, opts, email, password)
	if err != nil {
		return nil, err
	}
	return managementOn(t), nil
}

func managementOn(t *infra.Transport) *Management {
	return usecase.NewManagementOn(t, t, func(apiKey, user string) (*App, error) {
		return usecase.NewAppOn(t.ServiceTransportFor(apiKey, user)), nil
	})
}

package tests

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	dify "github.com/langgenius/dify-go-sdk"
)

// The live harness: contract tests against a running Dify, so the claims in
// this package are checked against a server rather than a mock.
//
//	set -a; . ../dify-python-sdk/.env; set +a    # DIFY_HOST, DIFY_CONSOLE_EMAIL, DIFY_CONSOLE_PASSWORD
//	go test -run Live ./...
//
// It logs in with dify.LoginManagement, deploys the fixture apps in testdata/
// with Management.Apps.Deploy — import, publish, key — and deletes all of it
// afterwards, including what a crashed earlier run left behind, found by the
// sdk-go-harness prefix. So the harness is itself a test of Management. The
// fixture apps use template nodes, not model nodes, so a run costs nothing.
//
// It also mints a dataset key and exports it as DIFY_DATASET_API_KEY for the
// duration, so the knowledge tests that gate on it run too; the key is revoked
// at the end, because a workspace holds only ten and a leaked one fills the cap
// without the error ever saying so.

const harnessPrefix = "sdk-go-harness"

// live is what the harness set up, or why it could not.
var live struct {
	skip        string
	host        string
	management  *dify.Management
	workflowID  string
	workflowKey string
	chatKey     string
}

func TestMain(m *testing.M) {
	cleanup := setUpLive()
	code := m.Run()
	cleanup()
	os.Exit(code)
}

func requireLive(t *testing.T) {
	t.Helper()
	if live.skip != "" {
		t.Skip(live.skip)
	}
}

func setUpLive() func() {
	host := strings.TrimRight(os.Getenv(dify.EnvHost), "/")
	email, password := os.Getenv("DIFY_CONSOLE_EMAIL"), os.Getenv("DIFY_CONSOLE_PASSWORD")
	if host == "" || email == "" || password == "" {
		live.skip = "no Dify configured: set DIFY_HOST, DIFY_CONSOLE_EMAIL and DIFY_CONSOLE_PASSWORD"
		return func() {}
	}
	live.host = host
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	m, err := dify.LoginManagement(ctx, email, password, dify.WithHost(host))
	if err != nil {
		live.skip = "console login failed: " + err.Error()
		return func() {}
	}
	live.management = m
	sweep(ctx, m)

	var undo []func()
	cleanup := func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
	}
	for _, fixture := range []struct {
		file string
		id   *string
		key  *string
	}{{"testdata/workflow.yml", &live.workflowID, &live.workflowKey}, {"testdata/chatflow.yml", nil, &live.chatKey}} {
		dsl, err := os.ReadFile(fixture.file)
		if err != nil {
			live.skip = err.Error()
			return cleanup
		}
		d, err := m.Apps.Deploy(ctx, string(dsl), &dify.DeployParams{AcceptDSLVersion: true})
		if d.Imported && d.Created {
			appID := d.AppID
			undo = append(undo, func() { deleteApp(m, appID) })
		}
		if err == nil {
			err = d.Err(dify.StageRunnable)
		}
		if err != nil {
			live.skip = fmt.Sprintf("deploying %s: %v", fixture.file, err)
			return cleanup
		}
		if fixture.id != nil {
			*fixture.id = d.AppID
		}
		*fixture.key = d.APIKey
	}
	if os.Getenv(dify.EnvDatasetAPIKey) == "" {
		if key, err := m.DatasetKeys.Create(ctx); err == nil {
			os.Setenv(dify.EnvDatasetAPIKey, key.Token)
			undo = append(undo, func() {
				if err := m.DatasetKeys.Delete(context.Background(), key.ID); err != nil {
					fmt.Fprintln(os.Stderr, "live harness: could not revoke dataset key", key.ID, err)
				}
				os.Unsetenv(dify.EnvDatasetAPIKey)
			})
		} else {
			fmt.Fprintln(os.Stderr, "live harness: no dataset key:", err)
		}
	}
	return cleanup
}

func deleteApp(m *dify.Management, appID string) {
	if err := m.Apps.Delete(context.Background(), appID); err != nil {
		fmt.Fprintln(os.Stderr, "live harness: could not delete app", appID, err)
	}
}

// sweep deletes what a crashed earlier run left behind: apps, Agents,
// pipelines (through their knowledge bases) and skills, found by the prefix.
func sweep(ctx context.Context, m *dify.Management) {
	if page, err := m.Apps.List(ctx, &dify.AppListParams{Limit: 100, Name: harnessPrefix}); err == nil {
		for app, err := range page.All(ctx) {
			if err != nil {
				break
			}
			if strings.HasPrefix(app.Name, harnessPrefix) {
				deleteApp(m, app.ID)
			}
		}
	}
	if agents, err := m.Agents.List(ctx, &dify.AgentListParams{Name: harnessPrefix}); err == nil {
		for _, a := range agents {
			if strings.HasPrefix(a.Name, harnessPrefix) {
				deleteApp(m, a.AppID)
			}
		}
	}
	if pipelines, err := m.Pipelines.List(ctx); err == nil {
		for _, p := range pipelines {
			if strings.HasPrefix(p.Name, harnessPrefix) {
				_ = m.Pipelines.Delete(ctx, p.DatasetID)
			}
		}
	}
	if skills, err := m.Skills.List(ctx); err == nil {
		for _, s := range skills {
			if strings.HasPrefix(s.Name, harnessPrefix) {
				_ = m.Skills.Delete(ctx, s.ID, s.DisplayName)
			}
		}
	}
}

func liveApp(t *testing.T, key string) *dify.App {
	t.Helper()
	requireLive(t)
	a, err := dify.NewApp(dify.WithAPIKey(key), dify.WithBaseURL(live.host+"/v1"), dify.WithUser(harnessPrefix))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

package tests

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	dify "github.com/langgenius/dify-go-sdk"
)

// The harness deployed its fixtures with Management already, so what these
// add is the parts of it that setting up does not reach.

func TestLiveTheDeployedWorkflowIsListedFoundByNameAndExported(t *testing.T) {
	requireLive(t)
	ctx, m := liveCtx(t), live.management
	app, err := m.Apps.Retrieve(ctx, "sdk-go-harness-workflow")
	if err != nil || app.ID != live.workflowID || app.Mode != "workflow" {
		t.Fatalf("got %v %v", app, err)
	}
	byID, err := m.Apps.Retrieve(ctx, live.workflowID)
	if err != nil || byID.Name != "sdk-go-harness-workflow" {
		t.Fatalf("by id: %v %v", byID, err)
	}
	dsl, err := m.Apps.Export(ctx, live.workflowID, false)
	if err != nil || !strings.Contains(dsl, "sdk-go-harness-workflow") {
		t.Fatalf("export: %v %.80q", err, dsl)
	}
}

func TestLiveAMintedKeyIsListedAndCanBeRevoked(t *testing.T) {
	requireLive(t)
	ctx, m := liveCtx(t), live.management
	key, err := m.Apps.Keys.Create(ctx, live.workflowID)
	if err != nil || !strings.HasPrefix(key.Token, "app-") || key.Masked() {
		t.Fatalf("minted %v %v", key, err)
	}
	defer m.Apps.Keys.Delete(context.Background(), live.workflowID, key.ID)
	keys, err := m.Apps.Keys.List(ctx, live.workflowID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, k := range keys {
		found = found || k.ID == key.ID
	}
	if !found {
		t.Errorf("the minted key %s is not listed", key.ID)
	}
	if err := m.Apps.Keys.Delete(ctx, live.workflowID, key.ID); err != nil {
		t.Fatal(err)
	}
}

func TestLiveADraftRunsWithoutBeingPublished(t *testing.T) {
	requireLive(t)
	run, err := live.management.Apps.RunDraft(liveCtx(t), live.workflowID, map[string]any{"word": "draft"}, nil)
	if err != nil || !run.Succeeded() {
		t.Fatalf("got %+v %v", run, err)
	}
}

func TestLiveATemporaryAppIsRunnableAndGoneOnceDeleted(t *testing.T) {
	requireLive(t)
	ctx, m := liveCtx(t), live.management
	dsl, err := os.ReadFile("testdata/workflow.yml")
	if err != nil {
		t.Fatal(err)
	}
	app, err := m.Apps.Temporary(ctx, string(dsl), harnessPrefix+"-temp")
	if err != nil {
		t.Fatal(err)
	}
	client, err := app.Client(harnessPrefix)
	if err != nil {
		t.Fatal(err)
	}
	run, err := client.Workflows.Runs.Create(ctx, map[string]any{"word": "temporary"}, nil)
	if err != nil || !run.Succeeded() {
		t.Fatalf("the temporary app did not run: %+v %v", run, err)
	}
	if err := app.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Apps.Retrieve(ctx, app.ID()); !errors.Is(err, dify.ErrValidation) && !errors.Is(err, dify.ErrNotFound) {
		t.Errorf("a deleted app was still found: %v", err)
	}
}

func TestLiveAnImportDifyCannotReadIsReportedNotRaised(t *testing.T) {
	requireLive(t)
	d, err := live.management.Apps.Import(liveCtx(t), "this: is not an app", nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Imported || d.Indeterminate || d.Error == "" {
		t.Errorf("got %+v", d)
	}
}

func TestLiveAWorkflowWithNoTriggerNodesHasNoTriggers(t *testing.T) {
	requireLive(t)
	triggers, err := live.management.Apps.Triggers.List(liveCtx(t), live.workflowID)
	if err != nil || len(triggers) != 0 {
		t.Errorf("got %v %v", triggers, err)
	}
}

func TestLiveTheWorkspaceListingsAnswer(t *testing.T) {
	// These read shapes nothing else in the harness exercises; what is in
	// them depends on the workspace, so the test is that each one parses.
	requireLive(t)
	ctx, m := liveCtx(t), live.management
	if _, err := m.Agents.List(ctx); err != nil {
		t.Errorf("agents: %v", err)
	}
	if _, err := m.Pipelines.List(ctx); err != nil {
		t.Errorf("pipelines: %v", err)
	}
	if _, err := m.Skills.List(ctx); err != nil {
		t.Errorf("skills: %v", err)
	}
	providers, err := m.Models.Providers(ctx)
	if err != nil {
		t.Errorf("model providers: %v", err)
	}
	for _, p := range providers {
		if p.Provider == "" {
			t.Errorf("a provider with no name: %+v", p.Raw)
		}
	}
	if _, err := m.Models.Names(ctx, "llm"); err != nil {
		t.Errorf("model names: %v", err)
	}
	tools, err := m.Tools.Providers(ctx)
	if err != nil {
		t.Errorf("tool providers: %v", err)
	}
	for _, p := range tools {
		if p.Name == "" {
			t.Errorf("a tool provider with no name: %+v", p.Raw)
		}
	}
	plugins, err := m.Tools.Plugins(ctx)
	if err != nil {
		t.Errorf("plugins: %v", err)
	}
	for _, p := range plugins {
		if p.PluginID == "" || p.UniqueIdentifier == "" {
			t.Errorf("a plugin without its ids: %+v", p)
		}
	}
	t.Logf("%d model providers, %d tool providers, %d plugins", len(providers), len(tools), len(plugins))
}

func TestLiveADatasetKeyIsListedMasked(t *testing.T) {
	requireLive(t)
	ctx, m := liveCtx(t), live.management
	key, err := m.DatasetKeys.Create(ctx)
	if err != nil || !strings.HasPrefix(key.Token, "dataset-") {
		t.Fatalf("got %v %v", key, err)
	}
	defer m.DatasetKeys.Delete(context.Background(), key.ID)
	keys, err := m.DatasetKeys.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if k.ID == key.ID && !k.Masked() {
			t.Errorf("the workspace listing should mask the token, got %q", k.Token)
		}
	}
}

package tests

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	dify "github.com/langgenius/dify-go-sdk"
	"github.com/langgenius/dify-go-sdk/internal/usecase"
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
	if _, err := m.Agents.List(ctx, nil); err != nil {
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

func TestLiveOpeningAPublishedWorkflowReportsItPublished(t *testing.T) {
	requireLive(t)
	// A key is passed so that opening mints none: an app holds ten.
	app, err := live.management.Apps.Open(liveCtx(t), live.workflowID, live.workflowKey)
	if err != nil {
		t.Fatal(err)
	}
	if !app.Deployment.Published || app.Deployment.Err(dify.StageRunnable) != nil {
		t.Errorf("the harness workflow is published, got %s", app.Deployment.Stage())
	}
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	dsl, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(dsl)
}

func TestLiveAPipelineDeploysIsListedExportedAndDeletedWithItsBase(t *testing.T) {
	requireLive(t)
	ctx, m := liveCtx(t), live.management
	d, err := m.Pipelines.Deploy(ctx, readFixture(t, "pipeline.yml"), &dify.PipelineDeployParams{AcceptDSLVersion: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.DatasetID != "" {
		defer m.Pipelines.Delete(context.Background(), d.DatasetID)
	}
	if err := d.Err(dify.StagePublished); err != nil {
		t.Fatal(err)
	}
	if d.PipelineID == "" || d.DatasetID == "" {
		t.Fatalf("a pipeline deploy reports both ids: %+v", d)
	}
	pipelines, err := m.Pipelines.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var listed *dify.PipelineSummary
	for _, p := range pipelines {
		if p.ID == d.PipelineID {
			listed = p
		}
	}
	if listed == nil || listed.DatasetID != d.DatasetID || !listed.Published {
		t.Errorf("the deployed pipeline is not listed as published: %+v", listed)
	}
	dsl, err := m.Pipelines.Export(ctx, d.PipelineID, false)
	if err != nil || !strings.Contains(dsl, "rag_pipeline") {
		t.Errorf("export: %v %.80q", err, dsl)
	}
	if err := m.Pipelines.Delete(ctx, d.DatasetID); err != nil {
		t.Fatal(err)
	}
}

func TestLiveAWebhookTriggerIsMadeOnPublishAndCanBePaused(t *testing.T) {
	requireLive(t)
	ctx, m := liveCtx(t), live.management
	d, err := m.Apps.Deploy(ctx, readFixture(t, "webhook.yml"), &dify.DeployParams{NoKey: true, AcceptDSLVersion: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.Imported {
		defer deleteApp(m, d.AppID)
	}
	if err := d.Err(dify.StagePublished); err != nil {
		t.Fatal(err)
	}
	triggers, err := m.Apps.Triggers.List(ctx, d.AppID)
	if err != nil || len(triggers) != 1 || triggers[0].Type != "trigger-webhook" || !triggers[0].Enabled() {
		t.Fatalf("got %v %v", triggers, err)
	}
	hook, err := m.Apps.Triggers.Webhook(ctx, d.AppID, triggers[0].NodeID)
	if err != nil || hook.WebhookID == "" || !strings.Contains(hook.URL, hook.WebhookID) {
		t.Fatalf("webhook %+v %v", hook, err)
	}
	paused, err := m.Apps.Triggers.SetEnabled(ctx, d.AppID, triggers[0].ID, false)
	if err != nil || paused.Enabled() {
		t.Fatalf("pausing: %+v %v", paused, err)
	}
	resumed, err := m.Apps.Triggers.SetEnabled(ctx, d.AppID, triggers[0].ID, true)
	if err != nil || !resumed.Enabled() {
		t.Fatalf("resuming: %+v %v", resumed, err)
	}
}

func TestLiveAnAgentPublishesOnTheRosterAndGetsAKey(t *testing.T) {
	requireLive(t)
	ctx, m := liveCtx(t), live.management
	d, err := m.Apps.Deploy(ctx, readFixture(t, "agent.yml"), &dify.DeployParams{AcceptDSLVersion: true})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Imported {
		t.Skipf("this Dify would not import an Agent: %s", d.Error)
	}
	defer deleteApp(m, d.AppID)
	if d.AppMode != "agent" {
		t.Fatalf("mode %q", d.AppMode)
	}
	if err := d.Err(dify.StageRunnable); err != nil {
		t.Fatal(err)
	}
	published, err := m.Agents.List(ctx, &dify.AgentListParams{PublicationStatus: "published"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range published {
		found = found || (a.AppID == d.AppID && a.Published)
	}
	if !found {
		t.Error("the deployed Agent is not on the roster as published")
	}
	// Why the mode has to be read: this is the route an Agent is refused on.
	if err := m.Apps.Publish(ctx, d.AppID); err == nil {
		t.Error("Dify published an Agent as a workflow")
	}
}

func TestLiveASkillIsImportedPublishedAndDeleted(t *testing.T) {
	requireLive(t)
	ctx, m := liveCtx(t), live.management
	name := fmt.Sprintf("%s-skill-%d", harnessPrefix, time.Now().UnixNano()%1_000_000)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("SKILL.md")
	fmt.Fprintf(w, "---\nname: %s\ndescription: A skill the Go SDK harness imports and deletes.\n---\n\nSay hello.\n", name)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	skill, err := m.Skills.Import(ctx, dify.FileFromReader(name, &buf), nil)
	if skill != nil && skill.ID != "" {
		defer m.Skills.Delete(context.Background(), skill.ID, skill.DisplayName)
	}
	if err != nil {
		t.Fatal(err)
	}
	if skill.Name != name || !skill.Published() {
		t.Fatalf("imported %+v", skill)
	}
	if found, err := m.Skills.Retrieve(ctx, name); err != nil || found.ID != skill.ID {
		t.Fatalf("retrieve: %v %v", found, err)
	}
	if err := m.Skills.Delete(ctx, skill.ID, ""); err != nil {
		t.Fatalf("an unreferenced skill needs no confirmation: %v", err)
	}
	if _, err := m.Skills.Retrieve(ctx, name); err == nil {
		t.Error("a deleted skill is still listed")
	}
}

func TestLiveASessionRenewsWithItsRefreshTokenAndEndsOnLogout(t *testing.T) {
	requireLive(t)
	ctx := liveCtx(t)
	// A session of its own: logging out ends the account's refresh token,
	// which the harness's session must not depend on.
	m, err := dify.LoginManagement(ctx, os.Getenv("DIFY_CONSOLE_EMAIL"), os.Getenv("DIFY_CONSOLE_PASSWORD"), dify.WithHost(live.host))
	if err != nil {
		t.Fatal(err)
	}
	// Renewed within the second of the login, Dify hands back the same access
	// token — its claims are the account and an expiry in whole seconds — so
	// the test is that the renewal answers and the session still works, not
	// that the token changed. This test is what found that out.
	if err := wire(usecase.ManagementPort(m)).ForceRefresh(ctx); err != nil {
		t.Fatalf("renewing through /refresh-token: %v", err)
	}
	if access, csrf := m.SessionTokens(); access == "" || csrf == "" {
		t.Fatal("the renewal left the session without its tokens")
	}
	if _, err := m.Apps.List(ctx, nil); err != nil {
		t.Fatalf("the renewed session was refused: %v", err)
	}
	if err := m.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Apps.List(ctx, nil); !errors.Is(err, dify.ErrValidation) {
		t.Errorf("a logged-out session was used: %v", err)
	}
}

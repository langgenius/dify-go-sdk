package dify

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func liveCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

func TestLiveTheIndexReportsTheRunningVersion(t *testing.T) {
	requireLive(t)
	info, err := Probe(liveCtx(t), live.host+"/v1")
	if err != nil || info.ServerVersion == "" || info.APIVersion != "v1" {
		t.Fatalf("got %+v %v", info, err)
	}
	t.Logf("talking to %s", info)
}

func TestLiveAWrongKeyIsAnAuthenticationErrorCarryingTheVersion(t *testing.T) {
	app := liveApp(t, "app-not-a-real-key-000000")
	_, err := app.Info(liveCtx(t))
	var apiErr *APIError
	if !errors.Is(err, ErrAuthentication) || !errors.As(err, &apiErr) || apiErr.ServerVersion == "" {
		t.Errorf("got %v", err)
	}
}

func TestLiveAWorkflowAppDescribesItself(t *testing.T) {
	app := liveApp(t, live.workflowKey)
	ctx := liveCtx(t)
	info, err := app.Info(ctx)
	if err != nil || !info.IsWorkflow() {
		t.Fatalf("got %+v %v", info, err)
	}
	params, err := app.Parameters(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	word, ok := params.Input("word")
	note, _ := params.Input("note")
	if !ok || !word.Required || word.Type != "text-input" || note.Required || note.Type != "paragraph" {
		t.Errorf("inputs: %+v", params.Inputs)
	}
	if len(params.Required()) != 1 {
		t.Errorf("only word is required: %+v", params.Required())
	}
	if _, err := app.Site(ctx); err != nil {
		t.Errorf("site: %v", err)
	}
	if _, err := app.Meta(ctx, ""); err != nil {
		t.Errorf("meta: %v", err)
	}
}

func TestLiveABlockingRunReturnsItsOutputsAndIDs(t *testing.T) {
	app := liveApp(t, live.workflowKey)
	ctx := liveCtx(t)
	run, err := app.Workflows.Runs.Create(ctx, map[string]any{"word": "hello"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !run.Succeeded() || run.Outputs["out"] != "hello!" || run.Outputs["echo"] != "hello" || run.RunID == "" || run.TaskID == "" {
		t.Fatalf("got %+v", run)
	}
	back, err := app.Workflows.Runs.Retrieve(ctx, run.RunID)
	if err != nil || !back.Succeeded() || back.RunID != run.RunID || back.Outputs["out"] != "hello!" {
		t.Errorf("read back: %+v %v", back, err)
	}
}

func TestLiveAStreamedRunReportsEachNodeAndTheOutcome(t *testing.T) {
	app := liveApp(t, live.workflowKey)
	ctx := liveCtx(t)
	stream, err := app.Workflows.Runs.Stream(ctx, map[string]any{"word": "streamed"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var types []string
	for event, err := range stream.Events() {
		if err != nil {
			t.Fatal(err)
		}
		types = append(types, event.Type)
	}
	run := stream.FinalRun()
	shout, ran := run.Node("shout")
	if !run.Succeeded() || !ran || !shout.Succeeded() || shout.Outputs["output"] != "streamed!" || run.RunID == "" || run.TaskID == "" {
		t.Fatalf("got %+v (events %v)", run, types)
	}
	if !strings.Contains(strings.Join(types, ","), "workflow_started") || !strings.Contains(strings.Join(types, ","), "workflow_finished") {
		t.Errorf("events: %v", types)
	}

	// Reopening a finished run delivers its end and closes.
	again, err := app.Workflows.Runs.Events(ctx, run.RunID, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range again.Events() {
		if err != nil {
			t.Fatal(err)
		}
	}
	if reopened := again.FinalRun(); !reopened.Succeeded() {
		t.Errorf("reopened: %+v", reopened)
	}
}

func TestLiveAMissingRequiredInputIsRefused(t *testing.T) {
	app := liveApp(t, live.workflowKey)
	_, err := app.Workflows.Runs.Create(liveCtx(t), map[string]any{}, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !strings.Contains(apiErr.Message, "word") {
		t.Errorf("got %v", err)
	}
}

func TestLiveTheWrongRouteForTheModeSaysSo(t *testing.T) {
	app := liveApp(t, live.workflowKey)
	_, err := app.Chat.Messages.Create(liveCtx(t), "hi", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !strings.Contains(strings.ToLower(apiErr.Message), "mode") {
		t.Errorf("got %v", err)
	}
}

func TestLiveTheRunHistoryPages(t *testing.T) {
	app := liveApp(t, live.workflowKey)
	ctx := liveCtx(t)
	if _, err := app.Workflows.Runs.Create(ctx, map[string]any{"word": "logged"}, nil); err != nil {
		t.Fatal(err)
	}
	page, err := app.Workflows.Runs.Logs(ctx, &LogParams{Limit: 1, Status: "succeeded"})
	if err != nil || len(page.Items) != 1 || page.Total == nil || *page.Total < 1 {
		t.Fatalf("got %+v %v", page, err)
	}
	all, err := page.Collect(ctx)
	if err != nil || len(all) < 1 {
		t.Errorf("got %d %v", len(all), err)
	}
}

func TestLiveAFileUploadsAndComesBackAsAReference(t *testing.T) {
	app := liveApp(t, live.workflowKey)
	f, err := app.Files.Upload(liveCtx(t), FileFromReader("note.txt", strings.NewReader("hello from go")), "")
	if err != nil {
		t.Fatal(err)
	}
	if f.ID == "" || f.Extension != "txt" || f.Size != int64(len("hello from go")) {
		t.Errorf("got %+v", f)
	}
	if ref := f.Reference(""); ref["upload_file_id"] != f.ID || ref["transfer_method"] != "local_file" {
		t.Errorf("reference: %v", ref)
	}
}

func TestLiveAChatflowNeedsItsQueryAndItsInputs(t *testing.T) {
	app := liveApp(t, live.chatKey)
	ctx := liveCtx(t)
	msg, err := app.Chat.Messages.Create(ctx, "hello", &MessageParams{Inputs: map[string]any{"topic": "go"}})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Answer != "about go" || !msg.Succeeded() || msg.ConversationID == "" || msg.MessageID == "" {
		t.Fatalf("got %+v", msg)
	}
	t.Cleanup(func() { _ = app.Chat.Conversations.Delete(context.Background(), msg.ConversationID, "") })

	_, err = app.Chat.Messages.Create(ctx, "hello", nil)
	if err == nil {
		t.Error("the start input is still required alongside the query")
	}
}

func TestLiveAConversationCanBeStreamedReadBackRenamedAndDeleted(t *testing.T) {
	app := liveApp(t, live.chatKey)
	ctx := liveCtx(t)
	stream, err := app.Chat.Messages.Stream(ctx, "first", &MessageParams{Inputs: map[string]any{"topic": "streams"}})
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for piece, err := range stream.Text() {
		if err != nil {
			t.Fatal(err)
		}
		text.WriteString(piece)
	}
	msg := stream.FinalMessage()
	if text.String() != "about streams" || !msg.Succeeded() || msg.ConversationID == "" || msg.TaskID == "" {
		t.Fatalf("got %q %+v", text.String(), msg)
	}
	conv := msg.ConversationID

	// Dify orders history by created_at alone, stored to the second, so two
	// turns inside one second come back in either order.
	time.Sleep(1100 * time.Millisecond)
	second, err := app.Chat.Messages.Create(ctx, "second", &MessageParams{Inputs: map[string]any{"topic": "streams"}, ConversationID: conv})
	if err != nil || second.ConversationID != conv {
		t.Fatalf("continuing the thread: %+v %v", second, err)
	}

	history, err := app.Chat.Messages.List(ctx, conv, nil)
	if err != nil {
		t.Fatal(err)
	}
	var queries []string
	for _, h := range history.Items {
		queries = append(queries, h.Query)
	}
	if strings.Join(queries, ",") != "first,second" {
		t.Errorf("history keeps the queries, oldest first: %v", queries)
	}

	if err := app.Chat.Messages.Feedback(ctx, second.MessageID, Like, &FeedbackParams{Content: "nice"}); err != nil {
		t.Errorf("feedback: %v", err)
	}
	if err := app.Chat.Messages.Feedback(ctx, second.MessageID, NoRating, nil); err != nil {
		t.Errorf("revoking feedback: %v", err)
	}
	if _, err := app.Feedbacks(ctx, nil); err != nil {
		t.Errorf("feedbacks: %v", err)
	}

	page, err := app.Chat.Conversations.List(ctx, &ConversationListParams{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range page.Items {
		found = found || c.ID == conv
	}
	if !found {
		t.Errorf("the conversation should be listed")
	}
	renamed, err := app.Chat.Conversations.Rename(ctx, conv, "go harness thread", nil)
	if err != nil || renamed.Name != "go harness thread" {
		t.Errorf("rename: %+v %v", renamed, err)
	}
	if _, err := app.Chat.Conversations.Variables(ctx, conv, nil); err != nil {
		t.Errorf("variables: %v", err)
	}
	if err := app.Chat.Conversations.Delete(ctx, conv, ""); err != nil {
		t.Errorf("delete: %v", err)
	}
	if _, err := app.Chat.Messages.List(ctx, conv, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("a deleted conversation is not found, got %v", err)
	}
}

func TestLiveAnnotationsRoundTrip(t *testing.T) {
	app := liveApp(t, live.chatKey)
	ctx := liveCtx(t)
	created, err := app.Annotations.Create(ctx, "What is the go sdk?", "A port of the Python one.")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Annotations.Delete(context.Background(), created.ID) })
	updated, err := app.Annotations.Update(ctx, created.ID, "What is the Go SDK?", "A port.")
	if err != nil || updated.Answer != "A port." {
		t.Errorf("update: %+v %v", updated, err)
	}
	page, err := app.Annotations.List(ctx, &AnnotationListParams{Keyword: "Go SDK"})
	if err != nil || len(page.Items) == 0 {
		t.Errorf("list: %+v %v", page, err)
	}
}

func TestLiveDisablingAnnotationReplyStillNeedsTheEmbeddingFields(t *testing.T) {
	// Checks the claim SetReply's signature rests on: Dify validates the same
	// payload for enable and disable, so an empty disable is refused.
	app := liveApp(t, live.chatKey)
	_, err := app.t.call(liveCtx(t), &request{method: http.MethodPost, path: "/apps/annotation-reply/disable", body: map[string]any{}})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode < 400 || apiErr.StatusCode >= 500 {
		t.Errorf("an empty disable should be refused by Dify, got %v", err)
	}
}

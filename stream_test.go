package dify

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func streamApp(t *testing.T, events ...map[string]any) (*App, *fakeDify) {
	t.Helper()
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) { writeSSE(w, events...) })
	return f.app(t), f
}

func node(id, execID, status string, meta map[string]any) map[string]any {
	return map[string]any{"event": "node_finished", "task_id": "task-1", "workflow_run_id": "run-1", "data": map[string]any{
		"id": execID, "node_id": id, "node_type": "llm", "status": status, "outputs": map[string]any{"text": "x"},
		"execution_metadata": meta, "elapsed_time": 0.5,
	}}
}

func TestAStreamedRunIsAccumulatedIntoOneRun(t *testing.T) {
	app, f := streamApp(t,
		map[string]any{"event": "workflow_started", "task_id": "task-1", "workflow_run_id": "run-1", "data": map[string]any{"id": "run-1"}},
		map[string]any{"event": "ping"},
		node("llm", "e1", "succeeded", map[string]any{"total_tokens": 10, "prompt_tokens": 7, "completion_tokens": 3, "total_price": "0.0001", "currency": "USD"}),
		map[string]any{"event": "text_chunk", "data": map[string]any{"text": "Hel"}},
		map[string]any{"event": "text_chunk", "data": map[string]any{"text": "lo"}},
		map[string]any{"event": "workflow_finished", "data": map[string]any{"status": "succeeded", "outputs": map[string]any{"answer": "Hello"}, "total_tokens": 10}},
	)
	stream, err := app.Workflows.Runs.Stream(context.Background(), map[string]any{"q": "hi"}, nil)
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
	if strings.Contains(strings.Join(types, ","), "ping") {
		t.Error("keepalives are not events")
	}
	run := stream.FinalRun()
	if !run.Succeeded() || run.RunID != "run-1" || run.TaskID != "task-1" || run.Outputs["answer"] != "Hello" {
		t.Errorf("got %+v", run)
	}
	if strings.Join(run.Text, "") != "Hello" {
		t.Errorf("text_chunk carries its text one level down; got %q", run.Text)
	}
	// Tokens from the run total, price from the node that reported one.
	u := run.Usage()
	price, currency, err := u.TotalPrice()
	if u.TotalTokens != 10 || err != nil || price.String() != "0.0001" || currency != "USD" {
		t.Errorf("the price must survive the merge with a price-less total: %v", u)
	}
	if f.last(t).Body["response_mode"] != "streaming" || f.last(t).Header.Get("Accept") != "text/event-stream" {
		t.Errorf("request %+v", f.last(t))
	}
}

func TestAnErrorEventAfterA200EndsTheStreamWithAnError(t *testing.T) {
	// An HTTP 200 is not a successful run: Dify reports a mid-stream failure
	// as an event after the status line.
	app, _ := streamApp(t,
		map[string]any{"event": "workflow_started", "task_id": "t", "data": map[string]any{"id": "r"}},
		map[string]any{"event": "error", "status": 400, "code": "invalid_param", "message": "model quota exceeded"},
		map[string]any{"event": "workflow_finished", "data": map[string]any{"status": "succeeded"}},
	)
	stream, _ := app.Workflows.Runs.Stream(context.Background(), nil, nil)
	var got error
	count := 0
	for _, err := range stream.Events() {
		count++
		if err != nil {
			got = err
		}
	}
	var apiErr *APIError
	if !errors.As(got, &apiErr) || !apiErr.InStream || apiErr.Message != "model quota exceeded" {
		t.Fatalf("want an in-stream APIError, got %v", got)
	}
	if count != 2 || !stream.FinalRun().Failed() {
		t.Errorf("the stream should stop at the error and report the run failed; saw %d events, %+v", count, stream.FinalRun())
	}
}

func TestKeepErrorsYieldsTheErrorEventAsAnEvent(t *testing.T) {
	app, _ := streamApp(t, map[string]any{"event": "error", "message": "boom"})
	stream, _ := app.Workflows.Runs.Stream(context.Background(), nil, &RunParams{KeepErrors: true})
	for ev, err := range stream.Events() {
		if err != nil || ev.Type != "error" {
			t.Errorf("got %v %v", ev.Type, err)
		}
	}
	if stream.FinalRun().Error != "boom" {
		t.Errorf("got %+v", stream.FinalRun())
	}
}

func TestAPauseIsNeitherSuccessNorFailure(t *testing.T) {
	app, _ := streamApp(t,
		map[string]any{"event": "workflow_started", "task_id": "t", "data": map[string]any{"id": "r"}},
		map[string]any{"event": "human_input_required", "data": map[string]any{"node_id": "approve", "form_token": "tok-1"}},
		map[string]any{"event": "workflow_paused", "data": map[string]any{"reasons": []any{map[string]any{"node_id": "approve", "form_token": "tok-1"}}}},
	)
	stream, _ := app.Workflows.Runs.Stream(context.Background(), nil, nil)
	for range stream.Events() {
	}
	run := stream.FinalRun()
	if !run.Paused() || run.Succeeded() || run.Failed() {
		t.Errorf("paused, succeeded and failed are three questions: %+v", run)
	}
	if len(run.PendingForms) != 1 || run.PendingForms[0] != "tok-1" || run.PausedNodes[0] != "approve" {
		t.Errorf("the form should be recorded once: %+v %+v", run.PendingForms, run.PausedNodes)
	}
	token, err := FormToken(run)
	if err != nil || token != "tok-1" {
		t.Errorf("got %q %v", token, err)
	}
}

func TestAPauseWithoutATokenSaysWhyThereIsNothingToSubmit(t *testing.T) {
	// Dify omits the token for a form it means to be answered in its own UI.
	run := &WorkflowRun{Status: "paused", PausedNodes: []string{"review"}}
	_, err := FormToken(run)
	if err == nil || !strings.Contains(err.Error(), "review") || !strings.Contains(err.Error(), "display_in_ui") {
		t.Errorf("got %v", err)
	}
	if !(&WorkflowRun{Status: "paused"}).Paused() {
		t.Error("a run read back with Retrieve reports status paused and no forms; it is still paused")
	}
}

func TestAnsweringOneOfTwoFormsLeavesTheOtherWaiting(t *testing.T) {
	app, _ := streamApp(t,
		map[string]any{"event": "human_input_required", "data": map[string]any{"node_id": "a", "form_token": "tok-a"}},
		map[string]any{"event": "human_input_required", "data": map[string]any{"node_id": "b", "form_token": "tok-b"}},
		map[string]any{"event": "human_input_form_filled", "data": map[string]any{"node_id": "a"}},
	)
	stream, _ := app.Workflows.Runs.Stream(context.Background(), nil, nil)
	for range stream.Events() {
	}
	run := stream.FinalRun()
	if !run.Paused() || len(run.PendingForms) != 1 || run.PendingForms[0] != "tok-b" {
		t.Errorf("the filled event names a node, and only that form is settled: %+v", run.PendingForms)
	}
}

func TestAnExpiredFormFailsTheRun(t *testing.T) {
	app, _ := streamApp(t,
		map[string]any{"event": "human_input_required", "data": map[string]any{"node_id": "a", "form_token": "tok-a"}},
		map[string]any{"event": "human_input_form_timeout", "data": map[string]any{"node_id": "a"}},
	)
	stream, _ := app.Workflows.Runs.Stream(context.Background(), nil, nil)
	for range stream.Events() {
	}
	if run := stream.FinalRun(); !run.Failed() || run.Error == "" {
		t.Errorf("got %+v", run)
	}
}

func TestARedeliveredExecutionIsCountedOnce(t *testing.T) {
	meta := map[string]any{"total_tokens": 5}
	app, _ := streamApp(t,
		node("loop_body", "e1", "succeeded", meta),
		node("loop_body", "e2", "succeeded", meta),
		node("loop_body", "e2", "succeeded", meta), // a reconnect resends it
	)
	stream, _ := app.Workflows.Runs.Stream(context.Background(), nil, nil)
	for range stream.Events() {
	}
	run := stream.FinalRun()
	if len(run.RunsOf("loop_body")) != 2 || run.Usage().TotalTokens != 10 {
		t.Errorf("two passes, one resend: want 2 executions / 10 tokens, got %d / %d", len(run.RunsOf("loop_body")), run.Usage().TotalTokens)
	}
}

func TestAStreamThatStopsMidAnswerIsNotFinished(t *testing.T) {
	app, _ := streamApp(t,
		map[string]any{"event": "message", "task_id": "t", "message_id": "m", "conversation_id": "c", "answer": "Half an ans"},
	)
	stream, _ := app.Chat.Messages.Stream(context.Background(), "hi", nil)
	var text strings.Builder
	for piece, err := range stream.Text() {
		if err != nil {
			t.Fatal(err)
		}
		text.WriteString(piece)
	}
	msg := stream.FinalMessage()
	if text.String() != "Half an ans" || msg.Finished || msg.Succeeded() {
		t.Errorf("a truncated answer must not pass for a complete one: %+v", msg)
	}
	if msg.TaskID != "t" || msg.ConversationID != "c" || msg.MessageID != "m" {
		t.Errorf("ids: %+v", msg)
	}
}

func TestAModerationReplacementReplacesTheAnswer(t *testing.T) {
	app, _ := streamApp(t,
		map[string]any{"event": "message", "answer": "something rejected"},
		map[string]any{"event": "message_replace", "answer": "I can't help with that.", "reason": "moderation"},
		map[string]any{"event": "message_end", "metadata": map[string]any{"usage": map[string]any{"total_tokens": 4, "total_price": "0.00002", "currency": "USD"}}},
	)
	stream, _ := app.Chat.Messages.Stream(context.Background(), "hi", nil)
	for range stream.Events() {
	}
	msg := stream.FinalMessage()
	if msg.Answer != "I can't help with that." || msg.ReplacedReason != "moderation" {
		t.Errorf("appending would return the rejected text too: %q", msg.Answer)
	}
	if !msg.Succeeded() || msg.Usage().TotalTokens != 4 {
		t.Errorf("got %+v usage %v", msg, msg.Usage())
	}
}

func TestAStreamCanBeReadOnce(t *testing.T) {
	app, _ := streamApp(t, map[string]any{"event": "message", "answer": "x"})
	stream, _ := app.Chat.Messages.Stream(context.Background(), "hi", nil)
	for range stream.Events() {
	}
	for _, err := range stream.Events() {
		if !errors.Is(err, ErrValidation) {
			t.Errorf("a second read should say so, got %v", err)
		}
	}
}

func TestReopeningAFinishedRunKeepsDifysTotal(t *testing.T) {
	// Reopening a finished run delivers workflow_finished and nothing else.
	app, f := streamApp(t,
		map[string]any{"event": "workflow_finished", "workflow_run_id": "r", "data": map[string]any{"status": "succeeded", "total_tokens": 123}},
	)
	stream, err := app.Workflows.Runs.Events(context.Background(), "r", &EventsParams{ResumePaused: true})
	if err != nil {
		t.Fatal(err)
	}
	for range stream.Events() {
	}
	run := stream.FinalRun()
	if run.Usage().TotalTokens != 123 || run.NodeUsage().TotalTokens != 0 {
		t.Errorf("a 123-token run must not read as free: %v", run.Usage())
	}
	req := f.last(t)
	if req.Method != "GET" || req.Path != "/v1/workflow/r/events" || req.Query["continue_on_pause"][0] != "true" || req.Query["user"][0] != "alice" {
		t.Errorf("request %+v", req)
	}
}

func TestCollectRunReadsRawSSE(t *testing.T) {
	sse := "data: {\"event\":\"workflow_started\",\"task_id\":\"t\",\"data\":{\"id\":\"r\"}}\n\n" +
		"not a data line\n" +
		"data: {not json}\n" +
		"data: {\"event\":\"workflow_finished\",\"data\":{\"status\":\"succeeded\",\"outputs\":{\"a\":1}}}\n"
	run, err := CollectRun(strings.NewReader(sse))
	if err != nil || !run.Succeeded() || run.RunID != "r" {
		t.Errorf("a malformed line ends that line, not the stream: %+v %v", run, err)
	}
}

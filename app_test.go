package dify

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// TestEveryAppVerbReachesItsRoute pins each verb to the method, path and body
// Dify's controllers define, so a refactor cannot quietly point one elsewhere.
// The live harness checks the same routes against a server.
func TestEveryAppVerbReachesItsRoute(t *testing.T) {
	ctx := context.Background()
	threshold := 0.9
	cases := []struct {
		name   string
		call   func(a *App) error
		method string
		path   string
		body   map[string]any
		query  map[string]string
	}{
		{"info", func(a *App) error { _, err := a.Info(ctx); return err }, "GET", "/v1/info", nil, nil},
		{"parameters", func(a *App) error { _, err := a.Parameters(ctx, ""); return err }, "GET", "/v1/parameters", nil, map[string]string{"user": "alice"}},
		{"site", func(a *App) error { _, err := a.Site(ctx); return err }, "GET", "/v1/site", nil, nil},
		{"end user", func(a *App) error { _, err := a.EndUser(ctx, "eu1"); return err }, "GET", "/v1/end-users/eu1", nil, nil},
		{"chat create", func(a *App) error {
			_, err := a.Chat.Messages.Create(ctx, "hi", &MessageParams{ConversationID: "c1", WorkflowID: "w1", NoAutoName: true})
			return err
		}, "POST", "/v1/chat-messages", map[string]any{"query": "hi", "response_mode": "blocking", "user": "alice", "conversation_id": "c1", "workflow_id": "w1", "auto_generate_name": false}, nil},
		{"chat stop", func(a *App) error { return a.Chat.Messages.Stop(ctx, "task1", "") }, "POST", "/v1/chat-messages/task1/stop", map[string]any{"user": "alice"}, nil},
		{"feedback", func(a *App) error { return a.Chat.Messages.Feedback(ctx, "m1", Dislike, nil) }, "POST", "/v1/messages/m1/feedbacks", map[string]any{"rating": "dislike", "user": "alice"}, nil},
		{"suggested", func(a *App) error { _, err := a.Chat.Messages.Suggested(ctx, "m1", ""); return err }, "GET", "/v1/messages/m1/suggested", nil, map[string]string{"user": "alice"}},
		{"rename", func(a *App) error {
			_, err := a.Chat.Conversations.Rename(ctx, "c1", "", &RenameParams{AutoGenerate: true})
			return err
		}, "POST", "/v1/conversations/c1/name", map[string]any{"user": "alice", "auto_generate": true}, nil},
		{"delete conversation", func(a *App) error { return a.Chat.Conversations.Delete(ctx, "c1", "") }, "DELETE", "/v1/conversations/c1", map[string]any{"user": "alice"}, nil},
		{"variables", func(a *App) error {
			_, err := a.Chat.Conversations.Variables(ctx, "c1", &VariableParams{Name: "memory"})
			return err
		}, "GET", "/v1/conversations/c1/variables", nil, map[string]string{"variable_name": "memory"}},
		{"set variable", func(a *App) error { _, err := a.Chat.Conversations.SetVariable(ctx, "c1", "v1", 3, ""); return err }, "PUT", "/v1/conversations/c1/variables/v1", map[string]any{"value": float64(3), "user": "alice"}, nil},
		{"run pinned", func(a *App) error {
			_, err := a.Workflows.Runs.Create(ctx, map[string]any{"a": "b"}, &RunParams{WorkflowID: "wf1"})
			return err
		}, "POST", "/v1/workflows/wf1/run", map[string]any{"response_mode": "blocking", "user": "alice"}, nil},
		{"retrieve run", func(a *App) error { _, err := a.Workflows.Runs.Retrieve(ctx, "r1"); return err }, "GET", "/v1/workflows/run/r1", nil, nil},
		{"stop run", func(a *App) error { return a.Workflows.Runs.Stop(ctx, "task1", "") }, "POST", "/v1/workflows/tasks/task1/stop", map[string]any{"user": "alice"}, nil},
		{"completion", func(a *App) error { _, err := a.Completions.Create(ctx, map[string]any{"query": "q"}, nil); return err }, "POST", "/v1/completion-messages", map[string]any{"response_mode": "blocking", "user": "alice"}, nil},
		{"stop completion", func(a *App) error { return a.Completions.Stop(ctx, "task1", "") }, "POST", "/v1/completion-messages/task1/stop", map[string]any{"user": "alice"}, nil},
		{"download", func(a *App) error { _, _, err := a.Files.Download(ctx, "f1", true); return err }, "GET", "/v1/files/f1/preview", nil, map[string]string{"as_attachment": "true"}},
		{"form", func(a *App) error { _, err := a.Forms.Retrieve(ctx, "tok"); return err }, "GET", "/v1/form/human_input/tok", nil, nil},
		{"submit form", func(a *App) error { return a.Forms.Submit(ctx, "tok", map[string]any{"ok": true}, "approve", "") }, "POST", "/v1/form/human_input/tok", map[string]any{"action": "approve", "user": "alice"}, nil},
		{"annotation update", func(a *App) error { _, err := a.Annotations.Update(ctx, "an1", "q", "a"); return err }, "PUT", "/v1/apps/annotations/an1", map[string]any{"question": "q", "answer": "a"}, nil},
		{"annotation delete", func(a *App) error { return a.Annotations.Delete(ctx, "an1") }, "DELETE", "/v1/apps/annotations/an1", nil, nil},
		{"annotation reply", func(a *App) error {
			_, err := a.Annotations.SetReply(ctx, false, ReplySettings{EmbeddingProvider: "openai", EmbeddingModel: "te3", ScoreThreshold: threshold})
			return err
		}, "POST", "/v1/apps/annotation-reply/disable", map[string]any{"embedding_provider_name": "openai", "embedding_model_name": "te3", "score_threshold": 0.9}, nil},
		{"annotation reply status", func(a *App) error { _, err := a.Annotations.ReplyStatus(ctx, "j1", true); return err }, "GET", "/v1/apps/annotation-reply/enable/status/j1", nil, nil},
		{"speak", func(a *App) error {
			_, _, err := a.Audio.Speak(ctx, SpeakParams{MessageID: "m1", Voice: "alloy"})
			return err
		}, "POST", "/v1/text-to-audio", map[string]any{"message_id": "m1", "voice": "alloy", "user": "alice"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{}) })
			if err := tc.call(f.app(t)); err != nil {
				t.Fatal(err)
			}
			got := f.last(t)
			if got.Method != tc.method || got.Path != tc.path {
				t.Fatalf("want %s %s, got %s %s", tc.method, tc.path, got.Method, got.Path)
			}
			for k, v := range tc.body {
				if got.Body[k] != v {
					t.Errorf("body[%s] = %v, want %v (body %v)", k, got.Body[k], v, got.Body)
				}
			}
			for k, v := range tc.query {
				if q := got.Query[k]; len(q) == 0 || q[0] != v {
					t.Errorf("query[%s] = %v, want %s", k, q, v)
				}
			}
		})
	}
}

func TestRevokingFeedbackSendsANullRating(t *testing.T) {
	// A null rating is how Dify takes a rating back; leaving it out is not.
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{}) })
	_ = f.app(t).Chat.Messages.Feedback(context.Background(), "m1", NoRating, nil)
	if !strings.Contains(string(f.last(t).Raw), `"rating":null`) {
		t.Errorf("got %s", f.last(t).Raw)
	}
}

func TestInputsAreNeverSentAsNull(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{}) })
	_, _ = f.app(t).Workflows.Runs.Create(context.Background(), nil, nil)
	if !strings.Contains(string(f.last(t).Raw), `"inputs":{}`) {
		t.Errorf("Dify's payload model wants an object: %s", f.last(t).Raw)
	}
}

func TestParametersAreUnwrappedByTheNameAKeyTakes(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"user_input_form": []any{
				map[string]any{"text-input": map[string]any{"variable": "topic", "label": "Topic", "required": true, "max_length": 48}},
				map[string]any{"select": map[string]any{"variable": "tone", "label": "Tone", "options": []any{"short", "long"}}},
				map[string]any{"paragraph": map[string]any{"label": "no variable"}},
			},
			"speech_to_text":    map[string]any{"enabled": true},
			"opening_statement": "hi",
		})
	})
	p, err := f.app(t).Parameters(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	tone, _ := p.Input("tone")
	topic, _ := p.Input("topic")
	if len(p.Inputs) != 2 || tone.Type != "select" || strings.Join(tone.Options, ",") != "short,long" || topic.Label != "Topic" || *topic.MaxLength != 48 {
		t.Errorf("got %+v", p.Inputs)
	}
	if !p.Features["speech_to_text"] || len(p.Required()) != 1 {
		t.Errorf("features %v required %v", p.Features, p.Required())
	}
}

func TestABlockingPauseIsNotAFinishedMessage(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"event": "workflow_paused", "task_id": "t", "conversation_id": "c",
			"data": map[string]any{"reasons": []any{map[string]any{"node_id": "approve", "form_token": "tok"}}}})
	})
	msg, err := f.app(t).Chat.Messages.Create(context.Background(), "hi", nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Finished || msg.Succeeded() || !msg.Paused() || msg.PendingForms[0] != "tok" {
		t.Errorf("an answer nobody has given yet is not a success: %+v", msg)
	}
}

func TestABlockingPausedRunCarriesItsForms(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"task_id": "t", "workflow_run_id": "r",
			"data": map[string]any{"id": "r", "status": "paused", "reasons": []any{map[string]any{"node_id": "n", "form_token": "tok"}}}})
	})
	run, _ := f.app(t).Workflows.Runs.Create(context.Background(), nil, nil)
	if !run.Paused() || run.PendingForms[0] != "tok" || run.PausedNodes[0] != "n" {
		t.Errorf("got %+v", run)
	}
}

func TestStoppingWithoutATaskIDSaysWhy(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) { t.Error("nothing should be sent") })
	err := f.app(t).Workflows.Runs.Stop(context.Background(), "", "")
	if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "streamed") {
		t.Errorf("got %v", err)
	}
}

func TestAnnotationReplyRefusesMissingEmbeddingFields(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) { t.Error("nothing should be sent") })
	_, err := f.app(t).Annotations.SetReply(context.Background(), false, ReplySettings{})
	if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "even to disable") {
		t.Errorf("got %v", err)
	}
}

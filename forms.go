package dify

import (
	"context"
	"net/http"
	"strings"
)

// Form is a paused run's human-input form, as it should be shown to whoever
// fills it in.
type Form struct {
	Token     string
	Content   string
	Inputs    []map[string]any
	Actions   []map[string]any
	Defaults  map[string]any
	ExpiresAt *int64
	Raw       map[string]any
}

// Forms are the forms a paused run is waiting on.
//
// Waiting is not failing: a run that reaches a human-input node stops, its
// stream ends, and it resumes when the form comes back. WorkflowRun.Paused
// says so, and PendingForms carries the token.
type Forms struct{ api port }

// FormToken picks the token to answer a paused run with — the first of its
// PendingForms — or explains why there is none.
func FormToken(run *WorkflowRun) (string, error) {
	if len(run.PendingForms) > 0 {
		return run.PendingForms[0], nil
	}
	if run.Paused() {
		// Paused, but with nothing to submit against. Dify omits the token
		// for a form it means to be answered in its own UI; saying "not
		// waiting on a form" of a run that plainly is would send the caller
		// looking in the wrong place.
		nodes := strings.Join(run.PausedNodes, ", ")
		if nodes == "" {
			nodes = "a node"
		}
		return "", argError("this run is paused at %s, but Dify reported no form token for it — "+
			"the form is one it expects to be answered in its own UI (display_in_ui), and the API has nothing to submit against", nodes)
	}
	return "", argError("this run is not waiting on a form; WorkflowRun.Paused says whether it is")
}

// Retrieve fetches a form by its token.
func (f *Forms) Retrieve(ctx context.Context, token string) (*Form, error) {
	if token == "" {
		return nil, argError("no form token; dify.FormToken(run) picks it from a paused run")
	}
	o, err := f.api.call(ctx, &request{method: http.MethodGet, path: "/form/human_input/" + pathEscape(token)})
	if err != nil {
		return nil, err
	}
	return &Form{
		Token:     token,
		Content:   o.str("form_content"),
		Inputs:    o.maps("inputs"),
		Actions:   o.maps("user_actions"),
		Defaults:  o.obj("resolved_default_values").raw(),
		ExpiresAt: o.intPtr("expiration_time"),
		Raw:       o.raw(),
	}, nil
}

// Submit answers a form with one of its actions, which resumes the run.
//
// Forms are one-shot: the first submission wins and a second answers 412.
// Follow the resumed run with WorkflowRuns.Events.
func (f *Forms) Submit(ctx context.Context, token string, inputs map[string]any, action, user string) error {
	if token == "" {
		return argError("no form token; dify.FormToken(run) picks it from a paused run")
	}
	who, err := f.api.who(user)
	if err != nil {
		return err
	}
	_, err = f.api.call(ctx, &request{
		method: http.MethodPost,
		path:   "/form/human_input/" + pathEscape(token),
		body:   map[string]any{"inputs": orEmpty(inputs), "action": action, "user": who},
	})
	return err
}

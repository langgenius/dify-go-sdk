package usecase

import (
	"context"
	"net/http"
	"strings"

	"github.com/langgenius/dify-go-sdk/internal/codec"
	"github.com/langgenius/dify-go-sdk/internal/entity"
	"github.com/langgenius/dify-go-sdk/internal/kernel"
	"github.com/langgenius/dify-go-sdk/internal/port"
)

// Forms are the forms a paused run is waiting on.
//
// Waiting is not failing: a run that reaches a human-input node stops, its
// stream ends, and it resumes when the form comes back. WorkflowRun.Paused
// says so, and PendingForms carries the token.
type Forms struct{ api port.Port }

// FormToken picks the token to answer a paused run with — the first of its
// PendingForms — or explains why there is none.
func FormToken(run *entity.WorkflowRun) (string, error) {
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
		return "", kernel.ArgError("this run is paused at %s, but Dify reported no form token for it — "+
			"the form is one it expects to be answered in its own UI (display_in_ui), and the API has nothing to submit against", nodes)
	}
	return "", kernel.ArgError("this run is not waiting on a form; WorkflowRun.Paused says whether it is")
}

// Retrieve fetches a form by its token.
func (f *Forms) Retrieve(ctx context.Context, token string) (*entity.Form, error) {
	if token == "" {
		return nil, kernel.ArgError("no form token; dify.FormToken(run) picks it from a paused run")
	}
	o, err := f.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/form/human_input/" + port.PathEscape(token)})
	if err != nil {
		return nil, err
	}
	return codec.FormFrom(o, token), nil
}

// Submit answers a form with one of its actions, which resumes the run.
//
// Forms are one-shot: the first submission wins and a second answers 412.
// Follow the resumed run with WorkflowRuns.Events.
func (f *Forms) Submit(ctx context.Context, token string, inputs map[string]any, action, user string) error {
	if token == "" {
		return kernel.ArgError("no form token; dify.FormToken(run) picks it from a paused run")
	}
	who, err := f.api.Who(user)
	if err != nil {
		return err
	}
	_, err = f.api.Call(ctx, &port.Request{
		Method: http.MethodPost,
		Path:   "/form/human_input/" + port.PathEscape(token),
		Body:   map[string]any{"inputs": kernel.OrEmpty(inputs), "action": action, "user": who},
	})
	return err
}

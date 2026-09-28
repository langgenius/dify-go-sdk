package usecase

import (
	"context"
	"net/http"

	"github.com/langgenius/dify-go-sdk/internal/codec"
	"github.com/langgenius/dify-go-sdk/internal/entity"
	"github.com/langgenius/dify-go-sdk/internal/kernel"
	"github.com/langgenius/dify-go-sdk/internal/port"
)

// RunParams are the optional parts of starting a workflow run.
type RunParams struct {
	User  string
	Files []map[string]any
	// WorkflowID pins the run to a published version other than the current
	// one — how a caller runs the version they tested.
	WorkflowID string
	// KeepErrors yields an "error" event as an ordinary event instead of
	// ending the stream with an *APIError. Streams only.
	KeepErrors bool
}

// WorkflowRuns are runs of this app's workflow.
//
// Create waits for the run and returns it. Stream hands it back as it
// happens. Retrieve reads one back afterwards, and Stop ends one still going.
type WorkflowRuns struct{ api port.Port }

func (r *WorkflowRuns) prepare(inputs map[string]any, mode string, p *RunParams) (string, map[string]any, error) {
	if p == nil {
		p = &RunParams{}
	}
	user, err := r.api.Who(p.User)
	if err != nil {
		return "", nil, err
	}
	body := map[string]any{"inputs": kernel.OrEmpty(inputs), "response_mode": mode, "user": user}
	if p.Files != nil {
		body["files"] = p.Files
	}
	path := "/workflows/run"
	if p.WorkflowID != "" {
		path = "/workflows/" + port.PathEscape(p.WorkflowID) + "/run"
	}
	return path, body, nil
}

// Create runs the workflow and waits for it.
//
// A blocking run reports one workflow-wide token count and no per-node
// detail; the price and the node breakdown exist only on the stream.
func (r *WorkflowRuns) Create(ctx context.Context, inputs map[string]any, p *RunParams) (*entity.WorkflowRun, error) {
	path, body, err := r.prepare(inputs, "blocking", p)
	if err != nil {
		return nil, err
	}
	o, err := r.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: path, Body: body})
	if err != nil {
		return nil, err
	}
	return codec.RunFromBlocking(o), nil
}

// Stream runs the workflow and returns it as it happens. Close the stream
// when done; closing stops watching, not the run — see Stop.
func (r *WorkflowRuns) Stream(ctx context.Context, inputs map[string]any, p *RunParams) (*codec.WorkflowRunStream, error) {
	path, body, err := r.prepare(inputs, "streaming", p)
	if err != nil {
		return nil, err
	}
	events, err := r.api.Stream(ctx, &port.Request{Method: http.MethodPost, Path: path, Body: body})
	if err != nil {
		return nil, err
	}
	return codec.NewWorkflowRunStream(events, p == nil || !p.KeepErrors), nil
}

// Retrieve reads a run back by its id. A paused run read this way reports
// status "paused" and no forms: the tokens were raised on the stream.
func (r *WorkflowRuns) Retrieve(ctx context.Context, runID string) (*entity.WorkflowRun, error) {
	o, err := r.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/workflows/run/" + port.PathEscape(runID)})
	if err != nil {
		return nil, err
	}
	return codec.RunFromBlocking(o), nil
}

// EventsParams are the optional parts of reopening a run's stream.
type EventsParams struct {
	// User is the identifier the run was started with.
	User string
	// ResumePaused holds the stream open across a pause rather than ending at
	// it.
	ResumePaused bool
}

// Events reopens a run's stream — after a pause, or a dropped connection,
// which says nothing about the run. A run that already finished emits its
// final event and closes.
func (r *WorkflowRuns) Events(ctx context.Context, runID string, p *EventsParams) (*codec.WorkflowRunStream, error) {
	if p == nil {
		p = &EventsParams{}
	}
	user, err := r.api.Who(p.User)
	if err != nil {
		return nil, err
	}
	q := port.Params{}.Set("user", user).SetBool("include_state_snapshot", false).SetBool("continue_on_pause", p.ResumePaused)
	events, err := r.api.Stream(ctx, &port.Request{Method: http.MethodGet, Path: "/workflow/" + port.PathEscape(runID) + "/events", Query: q.Values()})
	if err != nil {
		return nil, err
	}
	return codec.NewWorkflowRunStream(events, true), nil
}

// Stop stops a run that is still going, by its TaskID — which is not the
// RunID, and is why a run carries both.
func (r *WorkflowRuns) Stop(ctx context.Context, taskID, user string) error {
	return stopTask(ctx, r.api, "/workflows/tasks/", taskID, user, "run")
}

// LogParams narrow the run history.
type LogParams struct {
	Page    int
	Limit   int
	Status  string
	Keyword string
	// Filters are any further query parameters Dify's log listing takes,
	// such as created_at__after.
	Filters map[string]string
}

// Logs is the app's run history, including runs this client never watched.
// Retrieve reads one back in full. Left as maps: a log entry's shape is
// open-ended.
func (r *WorkflowRuns) Logs(ctx context.Context, p *LogParams) (*codec.Page[map[string]any], error) {
	if p == nil {
		p = &LogParams{}
	}
	fetch := func(ctx context.Context, number int) (kernel.Object, error) {
		q := port.Params{}.SetInt("page", number).SetInt("limit", p.Limit).Set("status", p.Status).Set("keyword", p.Keyword)
		for k, v := range p.Filters {
			q.Set(k, v)
		}
		return r.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/workflows/logs", Query: q.Values()})
	}
	return codec.FetchByPage(ctx, kernel.Object.Raw, fetch, p.Page)
}

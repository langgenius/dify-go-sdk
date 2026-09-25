package dify

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"iter"
	"strings"
	"sync"
)

// RunEvent is one event from a stream, with the parts this SDK understands
// pulled out. Payload is always the whole thing Dify sent, so a field this SDK
// has never heard of still reaches the caller.
type RunEvent struct {
	// Type is Dify's event name: workflow_started, node_finished, message,
	// text_chunk, human_input_required, workflow_finished, message_end, ...
	Type    string
	Payload map[string]any
	// Execution is set on node_finished: which node ran, and what it produced.
	Execution *NodeExecution
	// FormToken is set on human_input_required when Dify gives one. It leaves
	// it out for a form it means to be answered in its own UI, so a pause is
	// not the same thing as a token.
	FormToken string
	// NodeID is the node this event is about, where Dify names one.
	NodeID string
	// Text is the piece of the answer, on message, agent_message and
	// text_chunk.
	Text string
}

// Data is the event's nested data object, or an empty map.
func (e RunEvent) Data() map[string]any { return object(e.Payload).obj("data") }

// Events that carry a piece of the answer, and the field holding it. A
// chatflow streams "message" with the text at the top level; a workflow app
// streams "text_chunk" with it one level down in data. Getting that wrong
// yields silence rather than an error.
var textFields = map[string]string{
	"message":       "answer",
	"agent_message": "answer",
	"text_chunk":    "text",
}

// decodeSSE reads one SSE line as an event payload, or nil when there is
// nothing in it — blanks, comments, keepalives, and anything that is not a
// JSON object. A malformed line ends that line, not the stream.
func decodeSSE(line []byte) object {
	body, ok := bytes.CutPrefix(line, []byte("data:"))
	if !ok {
		return nil
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var payload map[string]any
	if err := dec.Decode(&payload); err != nil || payload == nil {
		return nil
	}
	if asString(payload["event"]) == "ping" {
		return nil
	}
	return object(payload)
}

func textOf(payload object) string {
	field, ok := textFields[payload.str("event")]
	if !ok {
		return ""
	}
	if payload.has(field) {
		return payload.str(field)
	}
	return payload.obj("data").str(field)
}

func toRunEvent(payload object) RunEvent {
	data := payload.obj("data")
	name := payload.str("event")
	var execution *NodeExecution
	if name == "node_finished" {
		execution = &NodeExecution{
			NodeID:      data.str("node_id"),
			NodeType:    data.str("node_type"),
			Status:      data.str("status"),
			ExecutionID: data.str("id"),
			Index:       data.int("index"),
			Title:       data.str("title"),
			Inputs:      data.obj("inputs").raw(),
			Outputs:     data.obj("outputs").raw(),
			ProcessData: data.obj("process_data").raw(),
			Error:       data.str("error"),
			Usage:       usageFrom(data.obj("execution_metadata"), data["elapsed_time"]),
		}
	}
	return RunEvent{
		Type:      name,
		Payload:   payload.raw(),
		Execution: execution,
		FormToken: firstNonZero(data.str("form_token"), payload.str("form_token")),
		NodeID:    firstNonZero(data.str("node_id"), payload.str("node_id")),
		Text:      textOf(payload),
	}
}

// waitingForm is one form a run is stopped at, as the events describe it.
type waitingForm struct {
	nodeID string
	token  string
}

// formKey is what identifies the form an event is about: the node, where Dify
// names one — it names a node on every human-input event and a token on only
// some of them.
func formKey(e RunEvent) string {
	return firstNonZero(e.NodeID, object(e.Data()).str("form_id"))
}

// watcher accumulates what the events say, so a stream can be asked what the
// run came to once it is over.
type watcher struct {
	// Not "running": a stream that ends without a finish event leaves the
	// outcome unlearned, and calling that "running" invites a caller to wait
	// for something that is not coming.
	status     string
	outputs    map[string]any
	err        string
	nodes      map[string]NodeExecution
	executions []NodeExecution
	text       []string
	// The forms the run is waiting on, keyed by what identifies each, in the
	// order they were raised. Keyed because that is how Dify settles them:
	// the filled and timeout events name a node and carry no token, so a flat
	// list of tokens could only be cleared wholesale, and answering one of two
	// parallel forms dropped the other.
	waiting      map[string]waitingForm
	waitingOrder []string
	runID        string
	taskID       string
	convID       string
	messageID    string
	metadata     map[string]any
	createdAt    *int64
	finished     bool
	replaced     string
	reported     *Usage
	seen         map[string]bool
}

func newWatcher() *watcher {
	return &watcher{status: "unknown", nodes: map[string]NodeExecution{}, waiting: map[string]waitingForm{}, seen: map[string]bool{}}
}

func (w *watcher) wait(key string, form waitingForm, replace bool) {
	if _, exists := w.waiting[key]; exists {
		if replace {
			w.waiting[key] = form
		}
		return
	}
	w.waiting[key] = form
	w.waitingOrder = append(w.waitingOrder, key)
}

func (w *watcher) settle(key string) {
	delete(w.waiting, key)
	for i, k := range w.waitingOrder {
		if k == key {
			w.waitingOrder = append(w.waitingOrder[:i], w.waitingOrder[i+1:]...)
			break
		}
	}
}

func (w *watcher) pendingForms() []string {
	var out []string
	for _, key := range w.waitingOrder {
		if f := w.waiting[key]; f.token != "" {
			out = append(out, f.token)
		}
	}
	return out
}

func (w *watcher) pausedNodes() []string {
	var out []string
	for _, key := range w.waitingOrder {
		if f := w.waiting[key]; f.nodeID != "" {
			out = append(out, f.nodeID)
		}
	}
	return out
}

func (w *watcher) see(e RunEvent) {
	payload, data := object(e.Payload), object(e.Data())
	w.taskID = firstNonZero(w.taskID, payload.str("task_id"))
	w.convID = firstNonZero(w.convID, payload.str("conversation_id"))
	w.messageID = firstNonZero(w.messageID, payload.str("message_id"))
	w.runID = firstNonZero(w.runID, firstNonZero(data.str("workflow_run_id"), payload.str("workflow_run_id")))
	if e.Type == "workflow_started" && w.runID == "" {
		w.runID = data.str("id")
	}

	if e.Execution != nil {
		w.nodes[e.Execution.NodeID] = *e.Execution
		// Reopening a stream can redeliver an execution already seen.
		// Counting it twice would double its tokens; the execution id tells a
		// repeat of one node from a resend of the same execution.
		id := e.Execution.ExecutionID
		if id == "" || !w.seen[id] {
			w.executions = append(w.executions, *e.Execution)
			if id != "" {
				w.seen[id] = true
			}
		}
	}

	switch {
	case e.Type == "message_replace":
		// Output moderation rejected the answer and Dify sent a whole
		// replacement, which is also what it saved. It replaces what was
		// streamed; appending would hand back the rejected text with the
		// replacement stuck on the end.
		w.text = []string{firstNonZero(payload.str("answer"), data.str("answer"))}
		w.replaced = firstNonZero(payload.str("reason"), data.str("reason"))
	case e.Text != "":
		w.text = append(w.text, e.Text)
	}

	switch {
	case e.Type == "human_input_form_filled" || e.Type == "human_input_form_timeout":
		// Checked before a raise, because a form being settled is the
		// opposite of one being raised.
		w.settleForm(e)
	case e.Type == "human_input_required" || e.FormToken != "":
		w.wait(firstNonZero(formKey(e), e.FormToken), waitingForm{nodeID: e.NodeID, token: e.FormToken}, true)
		// Waiting is not finishing, and it is not failing either.
		w.status = "paused"
	case e.Type == "workflow_paused":
		// Reopening a stream on a paused run delivers this event without the
		// ones that led to it, so it carries the reasons itself.
		w.status = "paused"
		for _, reason := range data.objs("reasons") {
			node, token := reason.str("node_id"), reason.str("form_token")
			if key := firstNonZero(node, firstNonZero(reason.str("form_id"), token)); key != "" {
				w.wait(key, waitingForm{nodeID: node, token: token}, false)
			}
		}
		if outputs := data.obj("outputs"); len(outputs) > 0 {
			w.outputs = outputs.raw()
		}
		w.seeTotal(data)
	case e.Type == "workflow_finished":
		w.status = firstNonZero(data.str("status"), "unknown")
		w.outputs = data.obj("outputs").raw()
		w.err = data.str("error")
		w.finished = true
		// Reopening a finished run delivers this event and nothing else, so
		// without its total the run would read as free.
		w.seeTotal(data)
	case e.Type == "message_end":
		meta := data.obj("metadata")
		if len(meta) == 0 {
			meta = payload.obj("metadata")
		}
		w.metadata = meta.raw()
		w.finished = true
		if w.status == "unknown" {
			w.status = "succeeded"
		}
		w.seeTotal(meta.obj("usage"))
	case e.Type == "error":
		w.status = "failed"
		w.err = firstNonZero(data.str("message"), payload.str("message"))
		w.finished = true
	}
	if w.createdAt == nil {
		w.createdAt = payload.intPtr("created_at")
	}
}

// settleForm records one form answered or expired. The rest of the run is
// unaffected: another open form keeps it waiting.
func (w *watcher) settleForm(e RunEvent) {
	expired := e.Type == "human_input_form_timeout"
	key := firstNonZero(formKey(e), e.FormToken)
	switch _, known := w.waiting[key]; {
	case key != "" && known:
		w.settle(key)
	case key == "":
		// Nothing to match on. Clearing the lot is only right when there was
		// one form to begin with, but there is nothing better to go on.
		w.waiting = map[string]waitingForm{}
		w.waitingOrder = nil
	}
	if expired && w.err == "" {
		w.err = "the human-input form expired"
	}
	if len(w.waiting) > 0 {
		return
	}
	if w.status == "paused" {
		if expired {
			w.status = "failed"
		} else {
			w.status = "unknown"
		}
	}
}

func (w *watcher) seeTotal(source object) {
	if source.int("total_tokens") != 0 || truthy(source["total_price"]) {
		u := usageFrom(source, source["elapsed_time"])
		w.reported = &u
	}
}

func (w *watcher) run() *WorkflowRun {
	nodes := make(map[string]NodeExecution, len(w.nodes))
	for k, v := range w.nodes {
		nodes[k] = v
	}
	return &WorkflowRun{
		Status:         w.status,
		Outputs:        w.outputs,
		Nodes:          nodes,
		Executions:     append([]NodeExecution(nil), w.executions...),
		Error:          w.err,
		Text:           append([]string(nil), w.text...),
		RunID:          w.runID,
		TaskID:         w.taskID,
		ConversationID: w.convID,
		MessageID:      w.messageID,
		PendingForms:   w.pendingForms(),
		PausedNodes:    w.pausedNodes(),
		ReportedUsage:  w.reported,
	}
}

func (w *watcher) message() *Message {
	return &Message{
		Answer:         strings.Join(w.text, ""),
		MessageID:      w.messageID,
		ConversationID: w.convID,
		TaskID:         w.taskID,
		Metadata:       w.metadata,
		CreatedAt:      w.createdAt,
		Error:          w.err,
		Executions:     append([]NodeExecution(nil), w.executions...),
		PendingForms:   w.pendingForms(),
		Finished:       w.finished,
		ReplacedReason: w.replaced,
	}
}

// eventStream is the plumbing both streams share.
//
// Closing it stops watching. It does not stop the run — that is Stop, which
// Dify has its own endpoint for.
type eventStream struct {
	body         io.ReadCloser
	raiseOnError bool
	mu           sync.Mutex
	w            *watcher
	closeOnce    sync.Once
	used         bool
}

func newEventStream(body io.ReadCloser, raiseOnError bool) *eventStream {
	return &eventStream{body: body, raiseOnError: raiseOnError, w: newWatcher()}
}

// Close stops watching and releases the connection. The run on Dify keeps
// going; stop it with the resource's Stop if that is what you meant.
func (s *eventStream) Close() error {
	var err error
	s.closeOnce.Do(func() { err = s.body.Close() })
	return err
}

// Events yields each event as it arrives, then closes the stream.
//
// An "error" event is yielded as an *APIError with InStream set, and ends the
// stream: Dify reports a mid-stream failure after the 200 has already gone
// out, so a 200 is not a successful run. A dropped connection is yielded as
// the read error, and says nothing about the run — reopen it with
// WorkflowRuns.Events.
//
// A stream can be read once.
func (s *eventStream) Events() iter.Seq2[RunEvent, error] {
	return func(yield func(RunEvent, error) bool) {
		defer s.Close()
		s.mu.Lock()
		if s.used {
			s.mu.Unlock()
			yield(RunEvent{}, argError("this stream has already been read; a stream can be read once"))
			return
		}
		s.used = true
		s.mu.Unlock()

		reader := bufio.NewReaderSize(s.body, 64<<10)
		for {
			line, err := reader.ReadBytes('\n')
			if payload := decodeSSE(bytes.TrimRight(line, "\r\n")); payload != nil {
				event := toRunEvent(payload)
				s.mu.Lock()
				s.w.see(event)
				s.mu.Unlock()
				if s.raiseOnError && event.Type == "error" {
					yield(event, streamError(payload))
					return
				}
				if !yield(event, nil) {
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					yield(RunEvent{}, err)
				}
				return
			}
		}
	}
}

// Text yields just the answer, a piece at a time.
func (s *eventStream) Text() iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		for event, err := range s.Events() {
			if err != nil {
				yield("", err)
				return
			}
			if event.Text != "" && !yield(event.Text, nil) {
				return
			}
		}
	}
}

func streamError(payload object) *APIError {
	return &APIError{
		StatusCode: payload.int("status"),
		Code:       payload.str("code"),
		Message:    firstNonZero(payload.str("message"), firstNonZero(payload.str("code"), "stream error")),
		Body:       payload.raw(),
		InStream:   true,
	}
}

// WorkflowRunStream is a workflow run, watched as it happens.
//
//	stream, err := app.Workflows.Runs.Stream(ctx, inputs, nil)
//	if err != nil { ... }
//	defer stream.Close()
//	for event, err := range stream.Events() {
//		if err != nil { ... }
//		if event.Execution != nil { fmt.Println(event.Execution.NodeID) }
//	}
//	run := stream.FinalRun()
type WorkflowRunStream struct{ *eventStream }

// Snapshot is the run as it stands right now, mid-stream. A run still going
// has no outputs yet, which is different from one that finished without any.
func (s *WorkflowRunStream) Snapshot() *WorkflowRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.run()
}

// FinalRun is what the run came to, once the stream has been read. A run that
// paused reports "paused", not "succeeded". Called before the stream is read
// it is the same as Snapshot, which is rarely what "final" was meant to mean.
func (s *WorkflowRunStream) FinalRun() *WorkflowRun { return s.Snapshot() }

// MessageStream is a chat or completion message, watched as it is written.
//
//	stream, err := app.Chat.Messages.Stream(ctx, "Hello", nil)
//	if err != nil { ... }
//	defer stream.Close()
//	for piece, err := range stream.Text() {
//		if err != nil { ... }
//		fmt.Print(piece)
//	}
//	message := stream.FinalMessage()
type MessageStream struct{ *eventStream }

// Snapshot is the answer so far. Finished is false until Dify says the
// message is done.
func (s *MessageStream) Snapshot() *Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.message()
}

// FinalMessage is the message once the stream has been read. An answer that
// stopped arriving halfway comes back with Finished false rather than passing
// for a complete one.
func (s *MessageStream) FinalMessage() *Message { return s.Snapshot() }

// CollectRun reads a whole Dify event stream into one WorkflowRun — the same
// accumulation the streams do, for a caller holding the raw SSE bytes.
func CollectRun(r io.Reader) (*WorkflowRun, error) {
	s := newEventStream(io.NopCloser(r), false)
	for _, err := range s.Events() {
		if err != nil {
			return s.w.run(), err
		}
	}
	return s.w.run(), nil
}

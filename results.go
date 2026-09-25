package dify

import (
	"fmt"
	"sort"
	"strings"
)

// Run statuses Dify uses for a run that is over, however it went.
var settled = map[string]bool{"succeeded": true, "failed": true, "stopped": true, "partial-succeeded": true}

// NodeExecution is one execution of one node.
//
// Distinct from the node itself: a node inside an iteration or a loop runs
// once per item, and each pass is its own execution with its own usage.
// Treating the two as the same thing is what made a looped node report only
// its last pass.
type NodeExecution struct {
	NodeID   string
	NodeType string
	Status   string
	// ExecutionID is Dify's id for this execution, distinct from NodeID. A
	// node in a loop shares one NodeID across every pass and has a different
	// ExecutionID each time, which is what lets a reopened stream tell a
	// repeat from a resend.
	ExecutionID string
	// Index is where this execution came in the run.
	Index       int
	Title       string
	Inputs      map[string]any
	Outputs     map[string]any
	ProcessData map[string]any
	Error       string
	Usage       Usage
}

// Succeeded reports whether this execution succeeded.
func (n NodeExecution) Succeeded() bool { return n.Status == "succeeded" }

func (n NodeExecution) String() string { return fmt.Sprintf("%s (%s)", n.NodeID, n.Status) }

// WorkflowRun is one run of a workflow, as Dify reported it.
//
// Succeeded, Paused and Failed are three separate questions: a run waiting
// for a person has neither succeeded nor failed.
type WorkflowRun struct {
	Status  string
	Outputs map[string]any
	// Nodes is the last execution of each node, keyed by node id. Executions
	// has every one, repeats included.
	Nodes      map[string]NodeExecution
	Executions []NodeExecution
	Error      string
	// Text is the answer as it streamed, piece by piece, when it did.
	Text []string
	// RunID is Dify's id for this run: what reopens its event stream and reads
	// it back.
	RunID string
	// TaskID is what a stop request names. Not the RunID — which is why the
	// run carries both.
	TaskID string
	// ConversationID and MessageID are set for a chatflow.
	ConversationID string
	MessageID      string
	// PendingForms are the tokens of human-input forms the run is waiting on.
	PendingForms []string
	// PausedNodes are the nodes the run is waiting at. Not the same list as
	// PendingForms: Dify leaves the token out of a form it means to be
	// answered in its own UI, so a run can be waiting at a node with no token
	// to submit against.
	PausedNodes []string
	// ReportedUsage is what Dify said the whole run consumed, when it said
	// anything. Separate from the per-node figures on purpose: a blocking
	// response reports only this, a stream reports only the breakdown, and
	// reopening a finished run reports this alone.
	ReportedUsage *Usage
	// Raw is the blocking response as Dify sent it. Empty for a streamed run,
	// whose events each carry their own.
	Raw map[string]any
}

// Succeeded reports whether Dify reported the run as succeeded.
func (r *WorkflowRun) Succeeded() bool { return r.Status == "succeeded" }

// Failed reports whether Dify reported the run as failed. Distinct from a
// dropped connection, which says nothing about the run, and from a pause.
func (r *WorkflowRun) Failed() bool { return r.Status == "failed" }

// Finished reports whether the run reached an end state, either way.
func (r *WorkflowRun) Finished() bool { return settled[r.Status] }

// Paused reports whether the run stopped to wait for a person.
//
// Dify's own word settles it: a run read back with Retrieve reports
// status "paused" and no forms at all, so reading this off PendingForms alone
// would miss every pause this client had not watched happen.
func (r *WorkflowRun) Paused() bool {
	if r.Status == "paused" {
		return true
	}
	waiting := len(r.PendingForms) > 0 || len(r.PausedNodes) > 0
	return waiting && !settled[r.Status]
}

// Usage is what the run consumed.
//
// Merged from two sources, because neither is complete. Dify's figure for the
// whole run covers parts this client never watched but carries no price;
// the per-node figures carry both, for the nodes that were seen. Otherwise it
// sums Executions, so a node inside a loop counts once per pass.
func (r *WorkflowRun) Usage() Usage {
	observed := r.NodeUsage()
	if r.ReportedUsage == nil {
		return observed
	}
	return observed.mergedWith(*r.ReportedUsage)
}

// NodeUsage is summed over the node executions this client observed. Lower
// than Usage when the stream was joined late or reopened — the difference is
// what happened while nobody was watching.
func (r *WorkflowRun) NodeUsage() Usage {
	var total Usage
	if len(r.Executions) > 0 {
		for _, n := range r.Executions {
			total = total.Add(n.Usage)
		}
		return total
	}
	for _, n := range r.Nodes {
		total = total.Add(n.Usage)
	}
	return total
}

// RunsOf is every execution of one node, in order.
func (r *WorkflowRun) RunsOf(nodeID string) []NodeExecution {
	var out []NodeExecution
	for _, n := range r.Executions {
		if n.NodeID == nodeID {
			out = append(out, n)
		}
	}
	return out
}

// Node is the last execution of one node, and false when it did not run.
func (r *WorkflowRun) Node(nodeID string) (NodeExecution, bool) {
	n, ok := r.Nodes[nodeID]
	return n, ok
}

// Err is nil for a succeeded run, and otherwise an error naming what happened.
func (r *WorkflowRun) Err() error {
	if r.Succeeded() {
		return nil
	}
	detail := r.Error
	if detail == "" {
		var parts []string
		for _, n := range r.Nodes {
			if n.Error != "" {
				parts = append(parts, n.NodeID+": "+n.Error)
			}
		}
		sort.Strings(parts)
		detail = strings.Join(parts, "; ")
	}
	if detail == "" {
		detail = "no error detail"
	}
	return fmt.Errorf("dify: workflow run %s: %s", r.Status, detail)
}

// Message is one message a chat or completion app produced, and the thread it
// belongs to.
type Message struct {
	Answer string
	// MessageID is what feedback attaches to.
	MessageID string
	// ConversationID continues the thread.
	ConversationID string
	// TaskID is what a stop request names.
	TaskID string
	// Metadata is Dify's metadata block — retriever resources, usage, and
	// whatever a newer Dify adds — kept whole.
	Metadata  map[string]any
	CreatedAt *int64
	Error     string
	// Executions are node executions, for a chatflow.
	Executions []NodeExecution
	// PendingForms are the human-input forms this message is still waiting on.
	PendingForms []string
	// Finished is whether Dify said the message was done. A stream cut short
	// mid-answer never sets it, which separates the answer so far from the
	// answer.
	Finished bool
	// ReplacedReason is why output moderation replaced the answer, when it
	// did. Answer is then the replacement — what Dify saved — not what the
	// model wrote.
	ReplacedReason string
	// Raw is the blocking response as Dify sent it.
	Raw map[string]any
}

// Usage is what this message cost, from Dify's metadata or its node runs.
func (m *Message) Usage() Usage {
	if reported := object(m.Metadata).obj("usage"); len(reported) > 0 {
		return usageFrom(reported, reported["latency"])
	}
	var total Usage
	for _, e := range m.Executions {
		total = total.Add(e.Usage)
	}
	return total
}

// Paused reports whether a chatflow is waiting for a person.
func (m *Message) Paused() bool { return len(m.PendingForms) > 0 }

// Succeeded reports whether Dify finished this message without an error. An
// answer that stopped arriving halfway is not a success.
func (m *Message) Succeeded() bool { return m.Finished && m.Error == "" && !m.Paused() }

func (m *Message) String() string { return m.Answer }

// formTokens reads the forms a paused blocking answer is waiting on, from
// data.reasons[].form_token.
func formTokens(payload object) []string {
	source := payload
	if data := payload.obj("data"); data.has("reasons") {
		source = data
	}
	var tokens []string
	for _, reason := range source.objs("reasons") {
		if token := reason.str("form_token"); token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

// pausedNodes reads the nodes a paused blocking answer is waiting at.
func pausedNodes(payload object) []string {
	source := payload
	if data := payload.obj("data"); data.has("reasons") {
		source = data
	}
	var nodes []string
	for _, reason := range source.objs("reasons") {
		if node := reason.str("node_id"); node != "" {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

package entity

import (
	"fmt"
	"sort"
	"strings"

	"github.com/langgenius/dify-go-sdk/internal/kernel"
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
	if reported := kernel.Object(m.Metadata).Obj("usage"); len(reported) > 0 {
		return UsageFrom(reported, reported["latency"])
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

// ServerInfo is what the Service API says about itself, before any
// credential.
type ServerInfo struct {
	ServerVersion string
	APIVersion    string
	Welcome       string
}

func (s ServerInfo) String() string {
	return strings.TrimSpace(fmt.Sprintf("Dify %s (%s)", s.ServerVersion, s.APIVersion))
}

// AppInfo is what Dify says an app is.
type AppInfo struct {
	Name        string
	Mode        string
	Description string
	Tags        []string
	AuthorName  string
	Raw         map[string]any
}

// IsChat reports whether the app is served at /chat-messages.
func (i *AppInfo) IsChat() bool {
	switch i.Mode {
	case "chat", "advanced-chat", "agent-chat":
		return true
	}
	return false
}

// IsWorkflow reports whether the app is served at /workflows/run.
func (i *AppInfo) IsWorkflow() bool { return i.Mode == "workflow" }

// InputField is one field an app declares, as its start node defines it.
//
// Name is the key to put in inputs. Dify spells it "variable" and puts a
// separate label beside it for display; using the label as the key is the
// usual first mistake, because for a field created in the UI the two are
// often the same string and it works until someone renames one.
type InputField struct {
	Name  string
	Label string
	// Type is text-input, paragraph, select, number, file, file-list,
	// checkbox, json_object or external_data_tool.
	Type        string
	Required    bool
	Options     []string
	Default     any
	MaxLength   *int
	Description string
	Hidden      bool
	Raw         map[string]any
}

// AppParameters is what an app declares it takes, and what it has turned on.
type AppParameters struct {
	Inputs             []InputField
	OpeningStatement   string
	SuggestedQuestions []string
	// Features are the toggles, flattened: Dify sends each as
	// {"enabled": bool}.
	Features map[string]bool
	// FileUpload is what the app accepts as uploads.
	FileUpload map[string]any
	// SystemParameters are the deployment's own limits — file sizes in MB,
	// uploads per workflow.
	SystemParameters map[string]any
	Raw              map[string]any
}

// Input is the declared field with this name, and false when there is none.
func (p *AppParameters) Input(name string) (InputField, bool) {
	for _, f := range p.Inputs {
		if f.Name == name {
			return f, true
		}
	}
	return InputField{}, false
}

// Required is the fields a run will be rejected without.
func (p *AppParameters) Required() []InputField {
	var out []InputField
	for _, f := range p.Inputs {
		if f.Required {
			out = append(out, f)
		}
	}
	return out
}

// SiteSettings is the WebApp's own settings: what a visitor sees before
// typing anything.
type SiteSettings struct {
	Title                  string
	Description            string
	Icon                   string
	IconType               string
	IconBackground         string
	IconURL                string
	DefaultLanguage        string
	ChatColorTheme         string
	ChatColorThemeInverted bool
	InputPlaceholder       string
	Copyright              string
	PrivacyPolicy          string
	CustomDisclaimer       string
	ShowWorkflowSteps      bool
	UseIconAsAnswerIcon    bool
	Raw                    map[string]any
}

// Annotation is a question and the answer you want given for it.
type Annotation struct {
	ID        string
	Question  string
	Answer    string
	HitCount  int
	CreatedAt *int64
	Raw       map[string]any
}

// AnnotationReplyJob is the indexing job that turns annotation reply on or
// off. Enabling it embeds every annotation, which takes time — so Dify
// answers with a job rather than a result.
type AnnotationReplyJob struct {
	ID     string
	Status string
	Error  string
}

// Finished reports whether the job is over, either way.
func (j *AnnotationReplyJob) Finished() bool {
	switch j.Status {
	case "completed", "failed", "error":
		return true
	}
	return false
}

// Conversation is one thread, belonging to one user.
type Conversation struct {
	ID           string
	Name         string
	Status       string
	Introduction string
	Inputs       map[string]any
	CreatedAt    *int64
	UpdatedAt    *int64
	Raw          map[string]any
}

// UploadedFile is a file Dify has taken, and the reference that points at it.
type UploadedFile struct {
	ID        string
	Name      string
	Size      int64
	MimeType  string
	Extension string
	// CreatedBy is an end-user id; App.EndUser resolves it.
	CreatedBy string
	CreatedAt *int64
	Raw       map[string]any
}

// Reference is the mapping a run's or a message's inputs use to name this
// file. Dify takes a reference, not the bytes:
//
//	f, _ := app.Files.Upload(ctx, dify.FileFromPath("report.pdf"), nil)
//	app.Workflows.Runs.Create(ctx, map[string]any{"doc": f.Reference("document")}, nil)
//
// kind is document, image, audio, video or custom.
func (f *UploadedFile) Reference(kind string) map[string]any {
	if kind == "" {
		kind = "document"
	}
	return map[string]any{"transfer_method": "local_file", "upload_file_id": f.ID, "type": kind}
}

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

// HistoryMessage is one turn in a conversation, as Dify records it.
//
// Deliberately not a Message. History carries the Query that prompted the
// answer, the files attached, the feedback left and what it cost — none of
// which a fresh reply has — and it has no TaskID, because nothing is running
// to stop. Reusing one type for both dropped the query, leaving a transcript
// of answers to questions nobody could see.
type HistoryMessage struct {
	ID             string
	ConversationID string
	// Query is what the user said.
	Query  string
	Answer string
	Inputs map[string]any
	Files  []map[string]any
	// Feedback is "like", "dislike", or empty when nobody rated it.
	Feedback           string
	RetrieverResources []map[string]any
	AgentThoughts      []map[string]any
	Status             string
	Error              string
	CreatedAt          *int64
	Usage              Usage
	Raw                map[string]any
}

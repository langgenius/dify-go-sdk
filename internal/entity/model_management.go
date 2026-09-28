package entity

import (
	"fmt"
	"strings"

	"github.com/langgenius/dify-go-sdk/internal/kernel"
)

// Stage is a one-word summary of how far a deploy got, for printing and for
// Deployment.Err. It is derived from the separate facts on a Deployment, never
// stored: an earlier design kept one stage and let a failed import followed by
// a successful publish read as runnable.
type Stage string

const (
	// StageUnknown is a request that went out with no answer back. What Dify
	// did is unknown, so neither retrying nor cleaning up is obviously right.
	StageUnknown Stage = "unknown"
	// StageNotImported is a document Dify refused, or held for confirmation:
	// no draft exists.
	StageNotImported Stage = "not-imported"
	// StageDrafted is a draft on Dify that nothing runs yet. The Service API
	// runs the published version.
	StageDrafted Stage = "drafted"
	// StagePublished is a live version with no key in hand to call it with.
	StagePublished Stage = "published"
	// StageRunnable is published, with a Service-API key in hand.
	StageRunnable Stage = "runnable"
)

func stageOf(indeterminate, imported, published, keyed bool) Stage {
	switch {
	case indeterminate:
		return StageUnknown
	case !imported:
		return StageNotImported
	case !published:
		return StageDrafted
	case keyed:
		return StageRunnable
	}
	return StagePublished
}

// StageError is a deploy that stopped short of the stage it was asked for.
type StageError struct {
	Reached, Wanted Stage
	// Cause is what Dify said, or what failed, when there was something.
	Cause string
	// Aftermath is what is left on Dify, and what to do about it.
	Aftermath string
}

func (e *StageError) Error() string {
	cause := ""
	if e.Cause != "" {
		cause = ": " + e.Cause
	}
	return fmt.Sprintf("dify: deploy reached %s but %s was wanted%s. %s", e.Reached, e.Wanted, cause, e.Aftermath)
}

// Deployment is what an app deploy came to: which steps happened, as separate
// facts, and where it stopped if it stopped early.
//
//	d, err := management.Apps.Deploy(ctx, dsl, nil)
//	if err := d.Err(dify.StageRunnable); err != nil { ... }
//
// A failed step is reported here rather than as the call's error, because a
// half-done deploy is a state to act on — the app may exist, unpublished — and
// an error alone would lose which half was done.
type Deployment struct {
	// Imported is a draft existing on Dify. False means nothing was created.
	Imported bool
	// Published is a version being live, so the Service API will run it.
	Published bool
	AppID     string
	AppMode   string
	// Version is the version Dify published, when it reports one.
	Version string
	// Created is whether this deploy made the app, which is what makes
	// deleting it safe. Overwriting an app does not create it.
	Created bool
	// ImportID is the import Dify held for confirmation, when it held one.
	ImportID string
	// NeedsConfirmation is Dify holding the import over a DSL version
	// difference. Not a refusal: the import exists and Apps.Confirm
	// completes it.
	NeedsConfirmation  bool
	ImportedDSLVersion string
	CurrentDSLVersion  string
	// Error is why the deploy stopped, when it did.
	Error string
	// Indeterminate is a request that went out with no answer back. Not the
	// same as a failure: the app may exist.
	Indeterminate bool
	Warnings      []string
	// APIKey is the Service-API key on hand, minted or given. String and
	// GoString leave it out, so printing a deployment does not log a key.
	APIKey string
	Raw    map[string]any
}

// Stage is the one-word summary of the facts above.
func (d Deployment) Stage() Stage {
	return stageOf(d.Indeterminate, d.Imported, d.Published, d.APIKey != "")
}

// Runnable is published, with a key in hand.
func (d Deployment) Runnable() bool { return d.Published && d.APIKey != "" }

// Err is nil when the deploy got as far as want, and otherwise a *StageError
// saying what is left on Dify.
func (d Deployment) Err(want Stage) error {
	if !d.Indeterminate && reached(want, d.Imported, d.Published, d.Runnable()) {
		return nil
	}
	var aftermath string
	switch {
	case d.Indeterminate:
		aftermath = "Dify's answer never arrived, so whether the app exists is unknown — check the workspace before retrying."
	case d.Imported:
		aftermath = fmt.Sprintf("App %s exists on Dify; delete it or retry the remaining steps.", d.AppID)
	case d.NeedsConfirmation:
		aftermath = fmt.Sprintf("Dify is holding this import over a DSL version difference; nothing is built yet. Complete it with Apps.Confirm(ctx, %q).", d.ImportID)
	default:
		aftermath = "Nothing was created on Dify."
	}
	return &StageError{Reached: d.Stage(), Wanted: want, Cause: d.Error, Aftermath: aftermath}
}

func (d Deployment) String() string { return strings.TrimSpace(string(d.Stage()) + " " + d.AppID) }

// GoString keeps %#v from printing the key.
func (d Deployment) GoString() string { return "dify.Deployment(" + d.String() + ")" }

// PipelineDeployment is what a knowledge pipeline deploy came to.
//
// A pipeline is not an app, and it carries two ids: PipelineID addresses the
// graph and DatasetID the knowledge base it fills. Deleting the knowledge base
// is what deletes the pipeline, which is why cleanup needs the second.
type PipelineDeployment struct {
	Imported bool
	// Published is a version being live, so documents go through it.
	Published  bool
	PipelineID string
	DatasetID  string
	ImportID   string
	// NeedsConfirmation is Dify holding the import over a DSL version
	// difference; Pipelines.Confirm completes it.
	NeedsConfirmation  bool
	ImportedDSLVersion string
	CurrentDSLVersion  string
	Error              string
	Indeterminate      bool
	Raw                map[string]any
}

// Stage is the one-word summary. A pipeline has no key: published is as far
// as it goes.
func (d PipelineDeployment) Stage() Stage {
	return stageOf(d.Indeterminate, d.Imported, d.Published, false)
}

// Err is nil when the deploy got as far as want. StagePublished is as far as a
// pipeline goes, so asking for StageRunnable asks for that.
func (d PipelineDeployment) Err(want Stage) error {
	if !d.Indeterminate && reached(want, d.Imported, d.Published, d.Published) {
		return nil
	}
	var aftermath string
	switch {
	case d.Indeterminate:
		aftermath = "Dify's answer never arrived, so whether the pipeline exists is unknown — check the workspace before retrying."
	case d.Imported:
		aftermath = fmt.Sprintf("Pipeline %s exists on Dify; delete its knowledge base (%s) or retry the publish.", d.PipelineID, d.DatasetID)
	case d.NeedsConfirmation:
		aftermath = fmt.Sprintf("Dify is holding this import over a DSL version difference (%s against the server's %s); nothing is built yet. Complete it with Pipelines.Confirm(ctx, %q).",
			orUnknown(d.ImportedDSLVersion), orUnknown(d.CurrentDSLVersion), d.ImportID)
	default:
		aftermath = "Nothing was created on Dify."
	}
	return &StageError{Reached: d.Stage(), Wanted: want, Cause: d.Error, Aftermath: aftermath}
}

func (d PipelineDeployment) String() string {
	return strings.TrimSpace(string(d.Stage()) + " " + d.PipelineID)
}

func reached(want Stage, imported, published, runnable bool) bool {
	switch want {
	case StageDrafted:
		return imported
	case StagePublished:
		return published
	case StageRunnable:
		return runnable
	}
	return true
}

func orUnknown(s string) string { return kernel.FirstNonZero(s, "unknown") }

// AppSummary is an app as the workspace lists it.
type AppSummary struct {
	ID          string
	Name        string
	Mode        string
	Description string
	Raw         map[string]any
}

func (a *AppSummary) String() string { return a.Name }

// APIKey is a Service-API key, for an app or for the workspace's knowledge
// bases.
//
// Dify shows the whole token when the key is minted. A listing of the
// workspace's dataset keys shows it masked — "datas...3f2a" — and a masked
// token authenticates as "Access token is invalid". (Dify 1.17 lists an app's
// keys in full.)
type APIKey struct {
	ID string
	// Token is the key itself, or its masked form from a listing that masks.
	// String masks it either way.
	Token     string
	Type      string
	CreatedAt *int64
	Raw       map[string]any
}

// Masked reports whether the token is the masked form a listing shows, and so
// cannot be used. Dify masks as the first five and last four characters
// around "...", or "***" for a short one.
func (k *APIKey) Masked() bool {
	return strings.Contains(k.Token, "...") || strings.Contains(k.Token, "*")
}

func (k *APIKey) String() string { return kernel.MaskSecret(k.Token) }

// GoString keeps %#v from printing the token.
func (k *APIKey) GoString() string { return "dify.APIKey(" + k.ID + ", " + k.String() + ")" }

// Trigger is a way a published workflow starts by itself. A trigger node in a
// draft is only a drawing: Dify makes the trigger when the workflow publishes.
type Trigger struct {
	ID           string
	Type         string
	Title        string
	NodeID       string
	Status       string
	ProviderName string
	Raw          map[string]any
}

// Enabled reports whether the trigger fires.
func (t *Trigger) Enabled() bool { return t.Status == "enabled" }

// WebhookTrigger is the endpoint Dify minted for a webhook trigger node.
type WebhookTrigger struct {
	ID        string
	WebhookID string
	URL       string
	// DebugURL runs the draft rather than the published version.
	DebugURL string
	NodeID   string
	Raw      map[string]any
}

// AgentSummary is an Agent as the workspace's roster reports it. Agents are
// kept off the app list.
//
// ID addresses the Agent on the roster, which is where it publishes; AppID is
// what every app-shaped call wants — export, delete, keys.
type AgentSummary struct {
	ID          string
	AppID       string
	Name        string
	Role        string
	Description string
	Published   bool
	Raw         map[string]any
}

func (a *AgentSummary) String() string { return a.Name }

// PipelineSummary is a knowledge pipeline as the workspace reports it: read off
// the knowledge base that owns it, since a pipeline has no listing of its own.
type PipelineSummary struct {
	// ID is the pipeline, whose graph is published.
	ID string
	// DatasetID is the knowledge base it fills, whose deletion deletes both.
	DatasetID   string
	Name        string
	Description string
	Published   bool
	Raw         map[string]any
}

func (p *PipelineSummary) String() string { return p.Name }

// WorkspaceSkill is an agent skill installed in the workspace.
type WorkspaceSkill struct {
	ID          string
	Name        string
	Description string
	DisplayName string
	// PublishedVersion is the latest published version, nil when there is
	// none. An Agent binds to a published version, so an unpublished skill
	// exists and nothing can use it.
	PublishedVersion *int
	ReferenceCount   int
	Raw              map[string]any
}

// Published reports whether a version exists for Agents to bind to.
func (s *WorkspaceSkill) Published() bool { return s.PublishedVersion != nil }

func (s *WorkspaceSkill) String() string { return s.Name }

// ToolProvider is a tool provider installed in the workspace, with its tools.
type ToolProvider struct {
	ID   string
	Name string
	// Type is builtin, plugin, api, workflow or mcp.
	Type   string
	Author string
	// Authorized is whether the provider's credentials are in place, which
	// is what decides whether its tools can run.
	Authorized bool
	// PluginUniqueIdentifier is name:version@hash for a plugin, and empty for
	// a builtin provider, which has none.
	PluginUniqueIdentifier string
	Tools                  []*Tool
	Raw                    map[string]any
}

func (p *ToolProvider) String() string { return p.Name }

// Tool is one tool a provider offers.
type Tool struct {
	Name       string
	Label      string
	Parameters []ToolParameter
	Raw        map[string]any
}

func (t *Tool) String() string { return t.Name }

// ToolParameter is one parameter a tool takes. Form is "llm" when the model
// fills it at run time, and "form" when the workflow sets it.
type ToolParameter struct {
	Name     string
	Type     string
	Required bool
	Form     string
	Default  any
	Label    string
}

// Plugin is a plugin installed in the workspace.
type Plugin struct {
	PluginID string
	// UniqueIdentifier is name:version@hash — what a workflow's dependency
	// list declares. Declaring another version makes Dify fetch it on import.
	UniqueIdentifier string
	Name             string
	Version          string
	Raw              map[string]any
}

func (p *Plugin) String() string { return p.PluginID }

package usecase

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"

	"github.com/langgenius/dify-go-sdk/internal/codec"
	"github.com/langgenius/dify-go-sdk/internal/entity"
	"github.com/langgenius/dify-go-sdk/internal/kernel"
	"github.com/langgenius/dify-go-sdk/internal/port"
)

// Apps are the workspace's apps, and the steps between a DSL document and a
// run.
//
// Dify keeps three things apart: importing writes a draft, publishing makes a
// version live, and the Service API runs the live version with a key. Import,
// Publish and Keys.Create are those steps; Deploy does all three and reports
// each as its own fact.
type Apps struct {
	api port.Port
	m   *Management

	// Keys are an app's Service-API keys.
	Keys *AppKeys
	// Triggers are the ways a published workflow starts by itself.
	Triggers *Triggers
}

// AppListParams narrow the workspace's apps. Zero values take Dify's defaults.
type AppListParams struct {
	Page  int
	Limit int
	// Mode is workflow, advanced-chat, chat, completion, agent-chat, agent
	// or channel.
	Mode string
	// Name matches apps whose name contains it.
	Name string
	// CreatedByMe keeps only the apps this account created.
	CreatedByMe bool
}

// List lists the workspace's apps. Agents are listed on their own, by Agents.
func (a *Apps) List(ctx context.Context, p *AppListParams) (*codec.Page[*entity.AppSummary], error) {
	if p == nil {
		p = &AppListParams{}
	}
	fetch := func(ctx context.Context, number int) (kernel.Object, error) {
		q := port.Params{}.SetInt("page", number).SetInt("limit", p.Limit).Set("mode", p.Mode).Set("name", p.Name)
		if p.CreatedByMe {
			q.SetBool("is_created_by_me", true)
		}
		return a.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/apps", Query: q.Values()})
	}
	return codec.FetchByPage(ctx, codec.AppSummaryFrom, fetch, p.Page)
}

// Retrieve finds one app by id, or by exact name.
//
// A name is matched exactly across every page — Dify's own filter matches a
// substring — and a name two apps share is refused with both ids, rather
// than answered with whichever came first.
func (a *Apps) Retrieve(ctx context.Context, nameOrID string) (*entity.AppSummary, error) {
	if nameOrID == "" {
		return nil, kernel.ArgError("name the app, by id or by name")
	}
	if isUUID(nameOrID) {
		o, err := a.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/apps/" + nameOrID})
		if err == nil {
			return codec.AppSummaryFrom(o), nil
		}
		if !errors.Is(err, kernel.ErrNotFound) {
			return nil, err
		}
		// Not an id after all: an app can be named anything.
	}
	page, err := a.List(ctx, &AppListParams{Limit: 100, Name: nameOrID})
	if err != nil {
		return nil, err
	}
	var found []*entity.AppSummary
	for app, err := range page.All(ctx) {
		if err != nil {
			return nil, err
		}
		if app.Name == nameOrID {
			found = append(found, app)
		}
	}
	switch len(found) {
	case 0:
		return nil, kernel.ArgError("no app called %q in this workspace. Apps.List shows what is there", nameOrID)
	case 1:
		return found[0], nil
	}
	ids := make([]string, len(found))
	for i, app := range found {
		ids[i] = app.ID
	}
	return nil, kernel.ArgError("%d apps are called %q; pass one of their ids: %v", len(found), nameOrID, ids)
}

// Open takes hold of an app that already exists, with a key to call it. It
// mints a key unless apiKey is given; each Open without one uses up one of
// the ten keys an app holds, so keep the key or revoke it.
func (a *Apps) Open(ctx context.Context, nameOrID, apiKey string) (*ManagedApp, error) {
	found, err := a.Retrieve(ctx, nameOrID)
	if err != nil {
		return nil, err
	}
	if apiKey == "" {
		key, err := a.Keys.Create(ctx, found.ID)
		if err != nil {
			return nil, err
		}
		apiKey = key.Token
	}
	return &ManagedApp{m: a.m, Deployment: entity.Deployment{Imported: true, Published: true, AppID: found.ID, AppMode: found.Mode, APIKey: apiKey}}, nil
}

// Export is an app's DSL, as the console's Export button writes it. Secrets
// are blanked unless includeSecret, so the default is safe to commit.
func (a *Apps) Export(ctx context.Context, appID string, includeSecret bool) (string, error) {
	o, err := a.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/apps/" + port.PathEscape(appID) + "/export", Query: port.Params{}.SetBool("include_secret", includeSecret).Values()})
	if err != nil {
		return "", err
	}
	dsl := codec.DSLFrom(o)
	if dsl == "" {
		return "", fmt.Errorf("dify: Dify returned no DSL for app %s", appID)
	}
	return dsl, nil
}

// Delete deletes an app and everything in it. There is no undo.
func (a *Apps) Delete(ctx context.Context, appID string) error {
	_, err := a.api.Call(ctx, &port.Request{Method: http.MethodDelete, Path: "/apps/" + port.PathEscape(appID)})
	return err
}

// ImportParams are the optional parts of importing a DSL.
type ImportParams struct {
	// AppID overwrites that app instead of creating one. Dify overwrites
	// workflow and advanced-chat apps only.
	AppID string
	// Name replaces the document's own name for the app.
	Name string
}

// Import writes a DSL document to Dify as a draft. Nothing runs it yet: the
// Service API runs the published version, so Publish is the next step, and
// Deploy does both.
//
// What Dify made of it is on the Deployment, not the error: an import Dify
// refused, one it is holding over a DSL version difference, and one whose
// answer never arrived are three states a caller acts on differently. The
// error is for arguments refused before sending.
func (a *Apps) Import(ctx context.Context, dsl string, p *ImportParams) (entity.Deployment, error) {
	if dsl == "" {
		return entity.Deployment{}, kernel.ArgError("the DSL document is empty")
	}
	if p == nil {
		p = &ImportParams{}
	}
	body := map[string]any{"mode": "yaml-content", "yaml_content": dsl}
	if p.AppID != "" {
		body["app_id"] = p.AppID
	}
	if p.Name != "" {
		body["name"] = p.Name
	}
	ans, indeterminate, err := importAnswer(a.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/apps/imports", Body: body}))
	if err != nil {
		return entity.Deployment{AppID: p.AppID, Indeterminate: indeterminate, Error: err.Error()}, nil
	}
	return deploymentFromImport(ans, p.AppID), nil
}

// Confirm completes an import Dify held over a DSL version difference. Until
// it is confirmed, a new app does not exist and an overwrite has not changed
// the app.
//
// held is the Deployment Import or Deploy returned. Created is claimed only
// when it shows a new app: confirming an overwrite used to report the
// caller's own app as one this call made, which is what makes deleting it
// look safe.
func (a *Apps) Confirm(ctx context.Context, held entity.Deployment) (entity.Deployment, error) {
	if held.ImportID == "" {
		return entity.Deployment{}, kernel.ArgError("this deployment carries no import id; Confirm takes the held result of Import or Deploy")
	}
	ans, indeterminate, err := importAnswer(a.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/apps/imports/" + port.PathEscape(held.ImportID) + "/confirm"}))
	if err != nil {
		return entity.Deployment{ImportID: held.ImportID, AppID: held.AppID, Indeterminate: indeterminate, Error: err.Error()}, nil
	}
	d := deploymentFromImport(ans, held.AppID)
	d.ImportID = held.ImportID
	return d, nil
}

// Publish publishes an app's draft workflow, making it the version the
// Service API runs. Dify serves this for workflow and advanced-chat apps; a
// chat or completion app keeps its configuration on itself and is live on
// import, and an Agent publishes on the roster — Deploy knows which is which.
func (a *Apps) Publish(ctx context.Context, appID string) error {
	_, err := a.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/apps/" + port.PathEscape(appID) + "/workflows/publish", Body: map[string]any{"marked_name": "", "marked_comment": ""}})
	return err
}

// DeployParams choose which of Deploy's steps happen.
type DeployParams struct {
	// AppID overwrites that app instead of creating one.
	AppID string
	// Name replaces the document's own name for the app.
	Name string
	// APIKey is a key already in hand, so none is minted.
	APIKey string
	// NoPublish stops at the draft.
	NoPublish bool
	// NoKey mints no key.
	NoKey bool
	// AcceptDSLVersion confirms an import Dify holds over a DSL version
	// difference, instead of stopping with NeedsConfirmation. Off by default:
	// the hold is Dify asking whether the difference is safe.
	AcceptDSLVersion bool
}

// Deploy imports a DSL, publishes it, and mints a key — reporting what
// actually happened, step by step, so "nothing was created", "the app exists
// but is unpublished" and "published, but no key" read differently:
//
//	d, err := m.Apps.Deploy(ctx, dsl, nil)
//	if err != nil { return err }            // an argument refused before sending
//	if err := d.Err(dify.StageRunnable); err != nil { ... }
//
// The mode it publishes by is the one Dify reports for the imported app. An
// Agent publishes on the roster, not at /workflows/publish, and does need
// publishing: minting its key first answers "Publish the Agent before
// enabling Web App or API access".
func (a *Apps) Deploy(ctx context.Context, dsl string, p *DeployParams) (entity.Deployment, error) {
	if p == nil {
		p = &DeployParams{}
	}
	d, err := a.Import(ctx, dsl, &ImportParams{AppID: p.AppID, Name: p.Name})
	if err != nil {
		return d, err
	}
	if d.NeedsConfirmation && p.AcceptDSLVersion {
		if d, err = a.Confirm(ctx, d); err != nil {
			return d, err
		}
	}
	if !d.Imported {
		return d, nil
	}
	if !p.NoPublish {
		switch d.AppMode {
		case "agent":
			snapshot, err := a.m.Agents.publishApp(ctx, d.AppID)
			if err != nil {
				d.Error = err.Error()
				d.Indeterminate = unanswered(err)
				return d, nil
			}
			d.Published, d.Version = true, snapshot
		case "chat", "completion", "agent-chat":
			// Live on import: the configuration is on the app itself, and
			// there is no draft to publish.
			d.Published = true
		default:
			if err := a.Publish(ctx, d.AppID); err != nil {
				// The app exists and is unpublished — or, with no answer,
				// perhaps published. Either way it is the caller's to act on.
				d.Error = err.Error()
				d.Indeterminate = unanswered(err)
				return d, nil
			}
			d.Published = true
		}
	}
	d.APIKey = p.APIKey
	if !p.NoKey && d.APIKey == "" {
		key, err := a.Keys.Create(ctx, d.AppID)
		if err != nil {
			d.Error = err.Error()
			return d, nil
		}
		d.APIKey = key.Token
	}
	return d, nil
}

// Temporary deploys an app to be deleted when done — what makes a test that
// calls a real app self-contained:
//
//	app, err := m.Apps.Temporary(ctx, dsl, "support-bot")
//	if err != nil { t.Fatal(err) }
//	defer app.Delete(context.Background())
//
// The app is named name plus a random suffix, so that an import whose answer
// never arrived can be found again by name — and only that app. A deploy that
// stops short of runnable is cleaned up before the error is returned.
func (a *Apps) Temporary(ctx context.Context, dsl, name string) (*ManagedApp, error) {
	if name == "" {
		return nil, kernel.ArgError("a temporary app needs a name to make unique, so that one whose import got no answer can still be found and deleted")
	}
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	unique := name + "-temporary-" + hex.EncodeToString(suffix)
	d, err := a.Deploy(ctx, dsl, &DeployParams{Name: unique})
	if err != nil {
		return nil, err
	}
	if d.Indeterminate && !d.Imported {
		d.Error += ". " + a.sweep(ctx, unique)
	}
	if err := d.Err(entity.StageRunnable); err != nil {
		if d.Imported && d.Created {
			// A cleanup failure must not replace the failure being reported.
			_ = a.Delete(ctx, d.AppID)
		}
		return nil, err
	}
	return &ManagedApp{m: a.m, Deployment: d}, nil
}

// sweep deletes every app called exactly name, and says what came of it. Only
// safe for a name made unique for the purpose.
func (a *Apps) sweep(ctx context.Context, name string) string {
	page, err := a.List(ctx, &AppListParams{Limit: 100, Name: name})
	if err != nil {
		return fmt.Sprintf("Looking for it failed too (%v); check the workspace for an app called %q", err, name)
	}
	deleted := 0
	for app, err := range page.All(ctx) {
		if err != nil {
			return fmt.Sprintf("Looking for it failed too (%v); check the workspace for an app called %q", err, name)
		}
		if app.Name == name && a.Delete(ctx, app.ID) == nil {
			deleted++
		}
	}
	if deleted > 0 {
		return fmt.Sprintf("An app called %q had been created anyway, and was deleted", name)
	}
	return fmt.Sprintf("No app called %q was created", name)
}

// DraftRunParams are the optional parts of running a draft.
type DraftRunParams struct {
	// Query is the message a chatflow's draft runs on — sys.query. Required
	// for an advanced-chat app.
	Query string
	// ConversationID continues a chatflow's debugging thread.
	ConversationID string
	// Mode is the app's mode, when known. Left out, it is looked up: the two
	// kinds of draft run on different routes and Dify refuses the wrong one.
	Mode string
	// Files to attach, in Dify's file mapping shape.
	Files []map[string]any
}

// RunDraft runs what an app's draft says, without publishing it — the
// editor's Run button, from code. The Service API runs only the published
// version, so this is how a document is tried before it is released. It runs
// as the console account, and appears in the app's logs as a debugging run.
func (a *Apps) RunDraft(ctx context.Context, appID string, inputs map[string]any, p *DraftRunParams) (*entity.WorkflowRun, error) {
	if p == nil {
		p = &DraftRunParams{}
	}
	mode := p.Mode
	if mode == "" {
		found, err := a.Retrieve(ctx, appID)
		if err != nil {
			return nil, err
		}
		mode = found.Mode
	}
	body := map[string]any{"inputs": kernel.OrEmpty(inputs)}
	if p.Files != nil {
		body["files"] = p.Files
	}
	var path string
	switch mode {
	case "workflow":
		path = "/apps/" + port.PathEscape(appID) + "/workflows/draft/run"
	case "advanced-chat":
		if p.Query == "" {
			return nil, kernel.ArgError("a chatflow's draft runs on a message: set DraftRunParams.Query, which becomes sys.query")
		}
		body["query"] = p.Query
		if p.ConversationID != "" {
			body["conversation_id"] = p.ConversationID
		}
		path = "/apps/" + port.PathEscape(appID) + "/advanced-chat/workflows/draft/run"
	default:
		return nil, kernel.ArgError("only workflow and advanced-chat apps have a draft to run; a %s app runs as it is configured — call it with its Service-API key", kernel.FirstNonZero(mode, "mode-less"))
	}
	events, err := a.api.Stream(ctx, &port.Request{Method: http.MethodPost, Path: path, Body: body})
	if err != nil {
		return nil, err
	}
	defer events.Close()
	return codec.CollectRun(events)
}

// AppKeys are an app's Service-API keys.
//
// An app holds ten keys, and minting an eleventh answers "Cannot create more
// than 10 API keys for this resource type" without saying which to revoke —
// so revoke a key when done with it.
type AppKeys struct{ api port.Port }

// List is the app's keys. Dify 1.17 lists them in full; an older one masked
// them, which APIKey.Masked tells.
func (k *AppKeys) List(ctx context.Context, appID string) ([]*entity.APIKey, error) {
	o, err := k.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/apps/" + port.PathEscape(appID) + "/api-keys"})
	if err != nil {
		return nil, err
	}
	return codec.APIKeysFrom(o), nil
}

// Create mints a key.
func (k *AppKeys) Create(ctx context.Context, appID string) (*entity.APIKey, error) {
	o, err := k.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/apps/" + port.PathEscape(appID) + "/api-keys"})
	if err != nil {
		return nil, err
	}
	return codec.APIKeyFrom(o), nil
}

// Delete revokes one key.
func (k *AppKeys) Delete(ctx context.Context, appID, keyID string) error {
	_, err := k.api.Call(ctx, &port.Request{Method: http.MethodDelete, Path: "/apps/" + port.PathEscape(appID) + "/api-keys/" + port.PathEscape(keyID)})
	return err
}

// Triggers are the ways a published workflow starts by itself: a schedule, a
// webhook, a plugin event. A trigger node in a draft is only a drawing; Dify
// makes the trigger, and a webhook's URL, when the workflow publishes.
type Triggers struct{ api port.Port }

// List is the app's triggers, enabled or not. Workflow apps only.
func (tr *Triggers) List(ctx context.Context, appID string) ([]*entity.Trigger, error) {
	o, err := tr.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/apps/" + port.PathEscape(appID) + "/triggers"})
	if err != nil {
		return nil, err
	}
	return codec.TriggersFrom(o), nil
}

// Webhook is the URL Dify minted for one webhook trigger node, named by the
// node's id.
func (tr *Triggers) Webhook(ctx context.Context, appID, nodeID string) (*entity.WebhookTrigger, error) {
	if nodeID == "" {
		return nil, kernel.ArgError("name the webhook trigger's node id; an app can have more than one trigger")
	}
	o, err := tr.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/apps/" + port.PathEscape(appID) + "/workflows/triggers/webhook", Query: port.Params{}.Set("node_id", nodeID).Values()})
	if err != nil {
		return nil, err
	}
	return codec.WebhookTriggerFrom(o), nil
}

// SetEnabled pauses or resumes one trigger without unpublishing the app.
func (tr *Triggers) SetEnabled(ctx context.Context, appID, triggerID string, enabled bool) (*entity.Trigger, error) {
	o, err := tr.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/apps/" + port.PathEscape(appID) + "/trigger-enable", Body: map[string]any{"trigger_id": triggerID, "enable_trigger": enabled}})
	if err != nil {
		return nil, err
	}
	return codec.TriggerFrom(o), nil
}

// ManagedApp is one app from the account's side: what management knows of it,
// and a way to call it.
type ManagedApp struct {
	m *Management
	// Deployment is how the app came to be held: its id, mode, key, and
	// whether this session created it.
	Deployment entity.Deployment
}

// ID is the app's id.
func (a *ManagedApp) ID() string { return a.Deployment.AppID }

// Client is an App keyed for this app. user is the default end-user
// identifier.
func (a *ManagedApp) Client(user string) (*App, error) {
	return a.m.AppClient(a.Deployment.APIKey, user)
}

// Export is the app's DSL, ready to commit.
func (a *ManagedApp) Export(ctx context.Context, includeSecret bool) (string, error) {
	return a.m.Apps.Export(ctx, a.ID(), includeSecret)
}

// Delete deletes the app.
func (a *ManagedApp) Delete(ctx context.Context) error { return a.m.Apps.Delete(ctx, a.ID()) }

func (a *ManagedApp) String() string { return "dify.ManagedApp(" + a.Deployment.String() + ")" }

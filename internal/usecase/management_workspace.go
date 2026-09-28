package usecase

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/langgenius/dify-go-sdk/internal/codec"
	"github.com/langgenius/dify-go-sdk/internal/entity"
	"github.com/langgenius/dify-go-sdk/internal/kernel"
	"github.com/langgenius/dify-go-sdk/internal/port"
)

// Agents are the workspace's Agents. Dify keeps them on a roster of their own,
// off the app list, and an Agent publishes there rather than as a workflow.
type Agents struct{ api port.Port }

// List is every Agent in the workspace.
func (ag *Agents) List(ctx context.Context) ([]*entity.AgentSummary, error) {
	fetch := func(ctx context.Context, number int) (kernel.Object, error) {
		return ag.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/agent", Query: port.Params{}.SetInt("page", number).SetInt("limit", 100).Values()})
	}
	page, err := codec.FetchByPage(ctx, codec.AgentSummaryFrom, fetch, 1)
	if err != nil {
		return nil, err
	}
	return page.Collect(ctx)
}

// Retrieve finds one Agent by exact name.
func (ag *Agents) Retrieve(ctx context.Context, name string) (*entity.AgentSummary, error) {
	all, err := ag.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range all {
		if a.Name == name {
			return a, nil
		}
	}
	return nil, kernel.ArgError("this workspace has no Agent named %q. Agents.List shows what is there", name)
}

// Publish publishes an Agent's draft, and returns the snapshot it made live.
// agentID is the roster id — AgentSummary.ID, not its AppID. versionNote may
// be empty.
func (ag *Agents) Publish(ctx context.Context, agentID, versionNote string) (string, error) {
	body := map[string]any{}
	if versionNote != "" {
		body["version_note"] = versionNote
	}
	o, err := ag.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/agent/" + port.PathEscape(agentID) + "/publish", Body: body})
	if err != nil {
		return "", err
	}
	return codec.AgentSnapshotFrom(o), nil
}

// publishApp publishes the Agent behind an app, found on the roster by its
// app id — which is all an import reports.
func (ag *Agents) publishApp(ctx context.Context, appID string) (string, error) {
	all, err := ag.List(ctx)
	if err != nil {
		return "", err
	}
	for _, a := range all {
		if a.AppID == appID {
			return ag.Publish(ctx, a.ID, "dify-go-sdk")
		}
	}
	return "", kernel.ArgError("app %s has no Agent on the roster, so there is no Agent draft to publish. An Agent app comes from importing an Agent DSL; a workflow publishes with Apps.Publish", appID)
}

// Pipelines are the workspace's knowledge pipelines.
//
// A pipeline is not an app: Dify serves it from /rag/pipelines, its DSL is
// kind: rag_pipeline at version 0.1.0, and importing one creates the knowledge
// base it fills. So two ids come back — the pipeline's and the knowledge
// base's — and deleting the knowledge base is what deletes both.
type Pipelines struct{ api port.Port }

// List is every knowledge base that has a pipeline behind it.
//
// Read off the knowledge bases, since a pipeline has no listing of its own,
// and walked to the end: most bases are not pipelines, so the only pipeline in
// a workspace can sit behind thirty bases that are not.
func (pl *Pipelines) List(ctx context.Context) ([]*entity.PipelineSummary, error) {
	fetch := func(ctx context.Context, number int) (kernel.Object, error) {
		return pl.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/datasets", Query: port.Params{}.SetInt("page", number).SetInt("limit", 100).Values()})
	}
	build := func(o kernel.Object) *entity.PipelineSummary {
		p, _ := codec.PipelineSummaryFrom(o)
		return p
	}
	page, err := codec.FetchByPage(ctx, build, fetch, 1)
	if err != nil {
		return nil, err
	}
	var out []*entity.PipelineSummary
	for p, err := range page.All(ctx) {
		if err != nil {
			return out, err
		}
		if p != nil {
			out = append(out, p)
		}
	}
	return out, nil
}

// PipelineImportParams are the optional parts of importing a pipeline.
type PipelineImportParams struct {
	// PipelineID overwrites that pipeline instead of creating one.
	PipelineID string
}

// Import writes a pipeline to Dify as a draft, creating its knowledge base.
// Nothing indexes through it until Publish; Deploy does both.
//
// The knowledge base is named after the document's rag_pipeline.name plus a
// number, which Dify appends whether or not anything collides — so find it by
// the DatasetID this returns, not by name. There is no name to pass: Dify
// accepts one on this route and never reads it.
func (pl *Pipelines) Import(ctx context.Context, dsl string, p *PipelineImportParams) (entity.PipelineDeployment, error) {
	if dsl == "" {
		return entity.PipelineDeployment{}, kernel.ArgError("the DSL document is empty")
	}
	body := map[string]any{"mode": "yaml-content", "yaml_content": dsl}
	if p != nil && p.PipelineID != "" {
		body["pipeline_id"] = p.PipelineID
	}
	ans, indeterminate, err := importAnswer(pl.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/rag/pipelines/imports", Body: body}))
	if err != nil {
		return entity.PipelineDeployment{Indeterminate: indeterminate, Error: err.Error()}, nil
	}
	return pipelineDeployment(ans), nil
}

// Confirm completes a pipeline import Dify held over a DSL version difference.
func (pl *Pipelines) Confirm(ctx context.Context, held entity.PipelineDeployment) (entity.PipelineDeployment, error) {
	if held.ImportID == "" {
		return entity.PipelineDeployment{}, kernel.ArgError("this deployment carries no import id; Confirm takes the held result of Import or Deploy")
	}
	ans, indeterminate, err := importAnswer(pl.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/rag/pipelines/imports/" + port.PathEscape(held.ImportID) + "/confirm"}))
	if err != nil {
		return entity.PipelineDeployment{ImportID: held.ImportID, Indeterminate: indeterminate, Error: err.Error()}, nil
	}
	d := pipelineDeployment(ans)
	d.ImportID = held.ImportID
	return d, nil
}

func pipelineDeployment(a *codec.ImportAnswer) entity.PipelineDeployment {
	d := entity.PipelineDeployment{
		Imported:           a.Succeeded(),
		PipelineID:         a.PipelineID,
		DatasetID:          a.DatasetID,
		ImportID:           a.ID,
		NeedsConfirmation:  a.Held(),
		ImportedDSLVersion: a.ImportedDSLVersion,
		CurrentDSLVersion:  a.CurrentDSLVersion,
		Raw:                a.Raw,
	}
	if !d.Imported {
		d.Error = heldReason(a, "Pipelines.Confirm(ctx, held)")
	}
	return d
}

// Publish publishes a pipeline's draft, making it the one documents go
// through.
func (pl *Pipelines) Publish(ctx context.Context, pipelineID string) error {
	_, err := pl.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/rag/pipelines/" + port.PathEscape(pipelineID) + "/workflows/publish", Body: map[string]any{}})
	return err
}

// PipelineDeployParams choose which of a pipeline deploy's steps happen.
type PipelineDeployParams struct {
	PipelineID string
	// NoPublish stops at the draft.
	NoPublish bool
	// AcceptDSLVersion confirms an import Dify holds over a DSL version
	// difference instead of stopping there.
	AcceptDSLVersion bool
}

// Deploy imports a pipeline and publishes it, reporting where it stopped.
func (pl *Pipelines) Deploy(ctx context.Context, dsl string, p *PipelineDeployParams) (entity.PipelineDeployment, error) {
	if p == nil {
		p = &PipelineDeployParams{}
	}
	d, err := pl.Import(ctx, dsl, &PipelineImportParams{PipelineID: p.PipelineID})
	if err != nil {
		return d, err
	}
	if d.NeedsConfirmation && p.AcceptDSLVersion {
		if d, err = pl.Confirm(ctx, d); err != nil {
			return d, err
		}
	}
	if !d.Imported || p.NoPublish {
		return d, nil
	}
	if err := pl.Publish(ctx, d.PipelineID); err != nil {
		// The pipeline exists and is unpublished — or, with no answer,
		// perhaps published.
		d.Error = err.Error()
		d.Indeterminate = unanswered(err)
		return d, nil
	}
	d.Published = true
	return d, nil
}

// Export is a pipeline's DSL, as the console's Export button writes it.
func (pl *Pipelines) Export(ctx context.Context, pipelineID string, includeSecret bool) (string, error) {
	o, err := pl.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/rag/pipelines/" + port.PathEscape(pipelineID) + "/exports", Query: port.Params{}.SetBool("include_secret", includeSecret).Values()})
	if err != nil {
		return "", err
	}
	dsl := codec.DSLFrom(o)
	if dsl == "" {
		return "", fmt.Errorf("dify: Dify returned no DSL for pipeline %s", pipelineID)
	}
	return dsl, nil
}

// Delete deletes a knowledge base, and with it the pipeline that fills it.
// Takes the dataset id, not the pipeline id: the knowledge base owns the
// pipeline, and there is no route that deletes a pipeline on its own.
func (pl *Pipelines) Delete(ctx context.Context, datasetID string) error {
	if datasetID == "" {
		return kernel.ArgError("Delete takes the knowledge base's id — DatasetID on a pipeline deploy — since deleting the base is what deletes its pipeline")
	}
	_, err := pl.api.Call(ctx, &port.Request{Method: http.MethodDelete, Path: "/datasets/" + port.PathEscape(datasetID)})
	return err
}

// WorkspaceModels are what the workspace can call, and whether the credentials
// for it are in place.
type WorkspaceModels struct{ api port.Port }

// Providers is every model provider installed here. Status is "active" when
// its credentials are in place and "no-configure" when they are not — which is
// what decides whether a node using it can run. The providers carry no models:
// List has those.
func (wm *WorkspaceModels) Providers(ctx context.Context) ([]*entity.ModelProvider, error) {
	o, err := wm.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/workspaces/current/model-providers"})
	if err != nil {
		return nil, err
	}
	return codec.ConfiguredProvidersFrom(o), nil
}

// List is the providers offering models of this type, each with its models.
// modelType is Dify's own word — llm, text-embedding, rerank, speech2text,
// tts, moderation — and empty means llm. Dify leaves out deprecated models
// and providers with none.
func (wm *WorkspaceModels) List(ctx context.Context, modelType string) ([]*entity.ModelProvider, error) {
	o, err := wm.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/workspaces/current/models/model-types/" + port.PathEscape(kernel.FirstNonZero(modelType, "llm"))})
	if err != nil {
		return nil, err
	}
	return codec.ModelProvidersFrom(o), nil
}

// Names is every usable model of this type, spelled provider:model — the way
// a workflow names one.
func (wm *WorkspaceModels) Names(ctx context.Context, modelType string) ([]string, error) {
	providers, err := wm.List(ctx, modelType)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, p := range providers {
		for _, m := range p.Models {
			if m.Usable() {
				names = append(names, p.Provider+":"+m.Name)
			}
		}
	}
	return names, nil
}

// Tools are the tool providers and plugins installed in the workspace.
type Tools struct{ api port.Port }

// Providers is every tool provider installed here, each with its tools.
//
// Dify lists a builtin or plugin provider's tools on a route of its own; one
// whose tools cannot be read is reported with none rather than failing the
// listing, since a provider can be installed and broken.
func (tl *Tools) Providers(ctx context.Context) ([]*entity.ToolProvider, error) {
	o, err := tl.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/workspaces/current/tool-providers"})
	if err != nil {
		return nil, err
	}
	var out []*entity.ToolProvider
	for _, item := range codec.ToolListFrom(o) {
		provider := codec.ToolProviderFrom(item, nil)
		tools := codec.ToolsListedWith(item)
		if len(tools) == 0 {
			tools = tl.toolsOf(ctx, provider)
		}
		out = append(out, codec.ToolProviderFrom(item, tools))
	}
	return out, nil
}

func (tl *Tools) toolsOf(ctx context.Context, p *entity.ToolProvider) []kernel.Object {
	switch p.Type {
	case "builtin", "plugin", "model":
	default:
		return nil
	}
	o, err := tl.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/workspaces/current/tool-provider/builtin/" + p.Name + "/tools"})
	if err != nil {
		return nil
	}
	return codec.ToolListFrom(o)
}

// Plugins is every plugin installed here.
func (tl *Tools) Plugins(ctx context.Context) ([]*entity.Plugin, error) {
	const pageSize = 256
	var out []*entity.Plugin
	for page := 1; ; page++ {
		o, err := tl.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/workspaces/current/plugin/list", Query: port.Params{}.SetInt("page", page).SetInt("page_size", pageSize).Values()})
		if err != nil {
			return out, err
		}
		plugins := codec.PluginsFrom(o)
		out = append(out, plugins...)
		// A short page is the end, and so is reaching the total when Dify
		// reports one. A missing total is not a total of zero: reading it as
		// one stopped the walk after the first full page.
		total, reported := codec.PluginTotal(o)
		if len(plugins) < pageSize || (reported && len(out) >= total) {
			return out, nil
		}
		if len(out) >= codec.MaxWalk {
			return out, &codec.PageLimitError{Limit: codec.MaxWalk}
		}
	}
}

// PluginIdentifier is the name:version@hash of an installed plugin, for a
// workflow's dependency list. Declaring any other version makes Dify fetch it
// on import, so read the real one rather than copying a hash from a
// marketplace page.
func (tl *Tools) PluginIdentifier(ctx context.Context, pluginID string) (string, error) {
	plugins, err := tl.Plugins(ctx)
	if err != nil {
		return "", err
	}
	var known []string
	for _, p := range plugins {
		if p.PluginID == pluginID && p.UniqueIdentifier != "" {
			return p.UniqueIdentifier, nil
		}
		known = append(known, p.PluginID)
	}
	sort.Strings(known)
	return "", kernel.ArgError("%q is not installed in this workspace (installed: %s). Install it in Dify first, or declare the identifier by hand", pluginID, kernel.FirstNonZero(strings.Join(known, ", "), "none"))
}

// Skills are the workspace's agent skills.
type Skills struct{ api port.Port }

// List is every skill installed in the workspace.
func (sk *Skills) List(ctx context.Context) ([]*entity.WorkspaceSkill, error) {
	fetch := func(ctx context.Context, number int) (kernel.Object, error) {
		return sk.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/workspaces/current/skills", Query: port.Params{}.SetInt("page", number).SetInt("limit", 100).Values()})
	}
	page, err := codec.FetchByPage(ctx, codec.WorkspaceSkillFrom, fetch, 1)
	if err != nil {
		return nil, err
	}
	return page.Collect(ctx)
}

// Retrieve finds one skill by exact name.
func (sk *Skills) Retrieve(ctx context.Context, name string) (*entity.WorkspaceSkill, error) {
	all, err := sk.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, s := range all {
		if s.Name == name {
			return s, nil
		}
	}
	return nil, kernel.ArgError("this workspace has no skill named %q. Import it first", name)
}

// SkillImportParams are the optional parts of importing a skill.
type SkillImportParams struct {
	// Draft leaves the skill unpublished. An Agent binds to a published
	// version, so an unpublished skill is in the workspace and no Agent can
	// use it — which is why publishing is the default.
	Draft bool
	// PublishNote is recorded on the published version.
	PublishNote string
}

// Import uploads a skill package — a zip holding a SKILL.md whose frontmatter
// names and describes it — and publishes it unless told not to.
func (sk *Skills) Import(ctx context.Context, file Upload, p *SkillImportParams) (*entity.WorkspaceSkill, error) {
	if p == nil {
		p = &SkillImportParams{}
	}
	if file.ContentType == "" {
		file.ContentType = "application/zip"
	}
	if filepath.Ext(file.Name) == "" {
		file.Name += ".zip"
	}
	part, err := file.part("file", false)
	if err != nil {
		return nil, err
	}
	o, err := sk.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/workspaces/current/skills/import", Form: &port.MultipartForm{File: part}})
	if err != nil {
		return nil, err
	}
	created := codec.WorkspaceSkillFrom(o)
	if p.Draft {
		return created, nil
	}
	if _, err := sk.Publish(ctx, created.ID, p.PublishNote); err != nil {
		return created, err
	}
	return sk.Retrieve(ctx, created.Name)
}

// Publish publishes a skill's draft and returns the new version number.
func (sk *Skills) Publish(ctx context.Context, skillID, note string) (int, error) {
	// Dify reads this body as JSON and refuses a missing one, so an empty
	// note still sends {}.
	body := map[string]any{}
	if note != "" {
		body["publish_note"] = note
	}
	o, err := sk.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/workspaces/current/skills/" + port.PathEscape(skillID) + "/publish", Body: body})
	if err != nil {
		return 0, err
	}
	return codec.SkillVersionFrom(o), nil
}

// Delete removes a skill from the workspace. An Agent bound to it loses it, so
// a skill that is referenced is deleted only with its display name as
// confirmation — the way the console asks for it. confirmation may be empty
// for a skill nothing references.
func (sk *Skills) Delete(ctx context.Context, skillID, confirmation string) error {
	body := map[string]any{}
	if confirmation != "" {
		body["confirmation_name"] = confirmation
	}
	_, err := sk.api.Call(ctx, &port.Request{Method: http.MethodDelete, Path: "/workspaces/current/skills/" + port.PathEscape(skillID), Body: body})
	return err
}

// DatasetKeys are the workspace's knowledge-base API keys: what
// dify.NewKnowledge takes. A workspace holds ten, and a listing masks them,
// so mint one when needed and revoke it when done.
type DatasetKeys struct{ api port.Port }

// List is the workspace's dataset keys, with their tokens masked.
func (dk *DatasetKeys) List(ctx context.Context) ([]*entity.APIKey, error) {
	o, err := dk.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/datasets/api-keys"})
	if err != nil {
		return nil, err
	}
	return codec.APIKeysFrom(o), nil
}

// Create mints a dataset key. This is the only moment its token is visible.
// datasetIDs limits the key to those knowledge bases; none means all of them.
func (dk *DatasetKeys) Create(ctx context.Context, datasetIDs ...string) (*entity.APIKey, error) {
	var body any
	if len(datasetIDs) > 0 {
		body = map[string]any{"dataset_ids": datasetIDs}
	}
	o, err := dk.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/datasets/api-keys", Body: body})
	if err != nil {
		return nil, err
	}
	return codec.APIKeyFrom(o), nil
}

// Delete revokes one dataset key.
func (dk *DatasetKeys) Delete(ctx context.Context, keyID string) error {
	_, err := dk.api.Call(ctx, &port.Request{Method: http.MethodDelete, Path: "/datasets/api-keys/" + port.PathEscape(keyID)})
	return err
}

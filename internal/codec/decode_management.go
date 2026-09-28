package codec

import (
	"github.com/langgenius/dify-go-sdk/internal/entity"
	"github.com/langgenius/dify-go-sdk/internal/kernel"
)

// ImportAnswer is what Dify made of an imported DSL document, for an app or a
// pipeline: both importers answer in this shape.
type ImportAnswer struct {
	ID                 string
	Status             string
	AppID              string
	AppMode            string
	PipelineID         string
	DatasetID          string
	ImportedDSLVersion string
	CurrentDSLVersion  string
	Error              string
	Warnings           []string
	Raw                map[string]any
}

// Succeeded is Dify reporting the import usable. Anything else failed, even
// when the HTTP status was 200. An older Dify omits the status, and an id
// coming back is then the only signal.
func (a *ImportAnswer) Succeeded() bool {
	switch a.Status {
	case "completed", "completed-with-warnings":
		return true
	case "":
		return a.AppID != "" || a.PipelineID != ""
	}
	return false
}

// Held is Dify holding the import over a DSL version difference, until it is
// confirmed. Not a refusal.
func (a *ImportAnswer) Held() bool { return a.Status == "pending" }

func ImportAnswerFrom(o kernel.Object) *ImportAnswer {
	return &ImportAnswer{
		ID:                 o.Str("id"),
		Status:             o.Str("status"),
		AppID:              o.Str("app_id"),
		AppMode:            o.Str("app_mode"),
		PipelineID:         o.Str("pipeline_id"),
		DatasetID:          o.Str("dataset_id"),
		ImportedDSLVersion: o.Str("imported_dsl_version"),
		CurrentDSLVersion:  o.Str("current_dsl_version"),
		Error:              o.Str("error"),
		Warnings:           warningsOf(o.List("warnings")),
		Raw:                o.Raw(),
	}
}

// warningsOf reads the import warnings, which Dify sends as strings from some
// versions and as objects with a message from others.
func warningsOf(items []any) []string {
	var out []string
	for _, item := range items {
		switch w := item.(type) {
		case string:
			out = append(out, w)
		case map[string]any:
			out = append(out, kernel.FirstNonZero(kernel.Object(w).Str("message"), kernel.AsString(w)))
		}
	}
	return out
}

// ImportRefusal reads an import Dify refused with a 400, which still carries
// the import's status in its body — so a failed import is a state to report,
// not an error to raise over.
func ImportRefusal(body map[string]any) (*ImportAnswer, bool) {
	o := kernel.Object(body)
	if _, ok := body["status"].(string); !ok {
		return nil, false
	}
	return ImportAnswerFrom(o), true
}

func AppSummaryFrom(o kernel.Object) *entity.AppSummary {
	return &entity.AppSummary{
		ID:          o.Str("id"),
		Name:        o.Str("name"),
		Mode:        o.Str("mode"),
		Description: o.Str("description"),
		Raw:         o.Raw(),
	}
}

func APIKeyFrom(o kernel.Object) *entity.APIKey {
	return &entity.APIKey{
		ID:        o.Str("id"),
		Token:     o.Str("token"),
		Type:      kernel.FirstNonZero(o.Str("type"), "app"),
		CreatedAt: o.IntPtr("created_at"),
		Raw:       o.Raw(),
	}
}

func APIKeysFrom(o kernel.Object) []*entity.APIKey { return buildAll(o.Objs("data"), APIKeyFrom) }

func TriggerFrom(o kernel.Object) *entity.Trigger {
	return &entity.Trigger{
		ID:           o.Str("id"),
		Type:         o.Str("trigger_type"),
		Title:        o.Str("title"),
		NodeID:       o.Str("node_id"),
		Status:       o.Str("status"),
		ProviderName: o.Str("provider_name"),
		Raw:          o.Raw(),
	}
}

func TriggersFrom(o kernel.Object) []*entity.Trigger { return buildAll(o.Objs("data"), TriggerFrom) }

func WebhookTriggerFrom(o kernel.Object) *entity.WebhookTrigger {
	return &entity.WebhookTrigger{
		ID:        o.Str("id"),
		WebhookID: o.Str("webhook_id"),
		URL:       o.Str("webhook_url"),
		DebugURL:  o.Str("webhook_debug_url"),
		NodeID:    o.Str("node_id"),
		Raw:       o.Raw(),
	}
}

func AgentSummaryFrom(o kernel.Object) *entity.AgentSummary {
	return &entity.AgentSummary{
		ID:          o.Str("id"),
		AppID:       o.Str("app_id"),
		Name:        o.Str("name"),
		Role:        o.Str("role"),
		Description: o.Str("description"),
		Published:   o.Bool("active_config_is_published"),
		Raw:         o.Raw(),
	}
}

// PipelineSummaryFrom reads a pipeline off the knowledge base that owns it,
// and reports false for a base with no pipeline behind it.
func PipelineSummaryFrom(o kernel.Object) (*entity.PipelineSummary, bool) {
	if o.Str("pipeline_id") == "" {
		return nil, false
	}
	return &entity.PipelineSummary{
		ID:          o.Str("pipeline_id"),
		DatasetID:   o.Str("id"),
		Name:        o.Str("name"),
		Description: o.Str("description"),
		Published:   o.Bool("is_published"),
		Raw:         o.Raw(),
	}, true
}

func WorkspaceSkillFrom(o kernel.Object) *entity.WorkspaceSkill {
	var version *int
	if v := o.IntPtr("latest_published_version_number"); v != nil {
		n := int(*v)
		version = &n
	}
	return &entity.WorkspaceSkill{
		ID:               o.Str("id"),
		Name:             o.Str("name"),
		Description:      o.Str("description"),
		DisplayName:      o.Str("display_name"),
		PublishedVersion: version,
		ReferenceCount:   o.Int("reference_count"),
		Raw:              o.Raw(),
	}
}

func WorkspaceSkillsFrom(o kernel.Object) []*entity.WorkspaceSkill {
	return buildAll(o.Objs("data"), WorkspaceSkillFrom)
}

// ToolProviderFrom reads a provider and the tools listed for it separately.
// A builtin provider reports an empty identifier; only a plugin carries one.
func ToolProviderFrom(o kernel.Object, tools []kernel.Object) *entity.ToolProvider {
	id := kernel.FirstNonZero(o.Str("id"), o.Str("name"))
	p := &entity.ToolProvider{
		ID:                     id,
		Name:                   kernel.FirstNonZero(o.Str("name"), id),
		Type:                   kernel.FirstNonZero(o.Str("type"), "builtin"),
		Author:                 o.Str("author"),
		Authorized:             boolOr(o, "is_team_authorization", true),
		PluginUniqueIdentifier: o.Str("plugin_unique_identifier"),
		Raw:                    o.Raw(),
	}
	for _, t := range tools {
		p.Tools = append(p.Tools, toolFrom(t))
	}
	return p
}

func toolFrom(o kernel.Object) *entity.Tool {
	t := &entity.Tool{
		Name:  o.Str("name"),
		Label: kernel.FirstNonZero(LabelOf(o["label"]), o.Str("name")),
		Raw:   o.Raw(),
	}
	for _, p := range o.Objs("parameters") {
		t.Parameters = append(t.Parameters, entity.ToolParameter{
			Name:     p.Str("name"),
			Type:     kernel.FirstNonZero(p.Str("type"), "string"),
			Required: p.Bool("required"),
			Form:     kernel.FirstNonZero(p.Str("form"), "form"),
			Default:  p["default"],
			Label:    LabelOf(p["label"]),
		})
	}
	return t
}

// ToolListFrom reads a listing that is a bare array on some routes and a
// {"data": [...]} envelope on others; kernel.Object wraps the bare one as
// data, so both read the same.
func ToolListFrom(o kernel.Object) []kernel.Object { return o.Objs("data") }

func PluginFrom(o kernel.Object) *entity.Plugin {
	declaration := o.Obj("declaration")
	return &entity.Plugin{
		PluginID:         o.Str("plugin_id"),
		UniqueIdentifier: o.Str("plugin_unique_identifier"),
		Name:             kernel.FirstNonZero(o.Str("name"), declaration.Str("name")),
		Version:          kernel.FirstNonZero(o.Str("version"), declaration.Str("version")),
		Raw:              o.Raw(),
	}
}

func PluginsFrom(o kernel.Object) []*entity.Plugin { return buildAll(o.Objs("plugins"), PluginFrom) }

// DSLFrom reads an export, which is the document under "data".
func DSLFrom(o kernel.Object) string { return o.Str("data") }

// AgentSnapshotFrom reads the snapshot an Agent publish made live.
func AgentSnapshotFrom(o kernel.Object) string { return o.Str("active_config_snapshot_id") }

// SkillVersionFrom reads the version number a skill publish made.
func SkillVersionFrom(o kernel.Object) int { return o.Int("version_number") }

// ConfiguredProvidersFrom reads the workspace's providers, which carry no
// models here and report whether their credentials are in place under
// custom_configuration — "active", or "no-configure".
func ConfiguredProvidersFrom(o kernel.Object) []*entity.ModelProvider {
	var out []*entity.ModelProvider
	for _, item := range o.Objs("data") {
		out = append(out, &entity.ModelProvider{
			Provider: item.Str("provider"),
			Label:    LabelOf(item["label"]),
			Status:   item.Obj("custom_configuration").Str("status"),
			Raw:      item.Raw(),
		})
	}
	return out
}

// PluginTotal is how many plugins Dify says are installed.
func PluginTotal(o kernel.Object) int { return o.Int("total") }

// ToolsListedWith is the tools a provider listing carried inline, which some
// providers do and others leave to a route of their own.
func ToolsListedWith(provider kernel.Object) []kernel.Object { return provider.Objs("tools") }

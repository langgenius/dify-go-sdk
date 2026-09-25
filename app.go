package dify

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// App is one Dify app, addressed by its Service-API key.
//
// The client holds the connection and the credential. What you can do hangs
// off it by what it acts on:
//
//	app, err := dify.NewApp(dify.WithAPIKey("app-…"), dify.WithUser("alice"))
//	msg, err := app.Chat.Messages.Create(ctx, "Hello", nil)
//	run, err := app.Workflows.Runs.Create(ctx, map[string]any{"text": "…"}, nil)
//
// An app has one mode, and Dify serves each mode on its own route — so
// app.Chat on a workflow app, or app.Workflows on a chatflow, answers "check
// if your app mode matches the right API route". Info reports which this is.
//
// An App is safe for concurrent use.
type App struct {
	t *transport

	Chat        *Chat
	Workflows   *Workflows
	Completions *Completions
	Files       *Files
	Annotations *Annotations
	Audio       *Audio
	Forms       *Forms
}

// Chat is the conversational half of an app: messages and the threads they
// form.
type Chat struct {
	Messages      *Messages
	Conversations *Conversations
}

// Workflows is the workflow half of an app: runs, and what they did.
type Workflows struct {
	Runs *WorkflowRuns
}

// NewApp builds a client for one app. The key comes from WithAPIKey, or
// DIFY_API_KEY; the base URL from WithBaseURL, DIFY_API_BASE_URL, or
// <DIFY_HOST>/v1. It sends nothing.
func NewApp(opts ...Option) (*App, error) {
	t, err := newTransport(opts, EnvAPIKey)
	if err != nil {
		return nil, err
	}
	return &App{
		t:           t,
		Chat:        &Chat{Messages: &Messages{t}, Conversations: &Conversations{t}},
		Workflows:   &Workflows{Runs: &WorkflowRuns{t}},
		Completions: &Completions{t},
		Files:       &Files{t},
		Annotations: &Annotations{t},
		Audio:       &Audio{t},
		Forms:       &Forms{t},
	}, nil
}

// OpenApp builds a client and asks Dify what the app is before returning it.
// Costs one request; worth it when the key comes from configuration and a
// wrong one should fail here rather than on the first run.
func OpenApp(ctx context.Context, opts ...Option) (*App, *AppInfo, error) {
	app, err := NewApp(opts...)
	if err != nil {
		return nil, nil, err
	}
	info, err := app.Info(ctx)
	if err != nil {
		return nil, nil, err
	}
	return app, info, nil
}

// BaseURL is the Service API root this client sends to.
func (a *App) BaseURL() string { return a.t.baseURL }

func (a *App) String() string {
	return fmt.Sprintf("dify.App(base_url=%q, api_key=%s)", a.t.baseURL, a.t.key)
}

// GoString keeps %#v from printing the key.
func (a *App) GoString() string { return a.String() }

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

func serverInfoFrom(o object) *ServerInfo {
	return &ServerInfo{ServerVersion: o.str("server_version"), APIVersion: o.str("api_version"), Welcome: o.str("welcome")}
}

// ServerInfo asks this client's Dify what it is. The Service API's index
// takes no credential and is the one place the running version is reliably
// reported: /openapi/v1/_version is off unless OPENAPI_ENABLED is set, and
// /console/api/version reports the latest released version rather than the
// one you are talking to.
func (a *App) ServerInfo(ctx context.Context) (*ServerInfo, error) {
	o, err := a.t.call(ctx, &request{method: http.MethodGet, path: "/", noAuth: true})
	if err != nil {
		return nil, err
	}
	return serverInfoFrom(o), nil
}

// Probe asks a Dify what version it is, with no client and no credential —
// for finding out whether a host is a Dify at all, since an unreachable host
// and a wrong key look the same once authenticated calls start. baseURL is
// the Service API root; empty resolves it the way NewApp does.
func Probe(ctx context.Context, baseURL string) (*ServerInfo, error) {
	t := &transport{
		baseURL: resolveBaseURL(baseURL),
		http:    &http.Client{},
		timeout: 5 * time.Second,
		logger:  newDiscardLogger(),
		sleep:   sleepCtx,
	}
	o, err := t.call(ctx, &request{method: http.MethodGet, path: "/", noAuth: true})
	if err != nil {
		return nil, err
	}
	return serverInfoFrom(o), nil
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

// Info is what this app is: its name, and the mode that decides its routes.
func (a *App) Info(ctx context.Context) (*AppInfo, error) {
	o, err := a.t.call(ctx, &request{method: http.MethodGet, path: "/info"})
	if err != nil {
		return nil, err
	}
	return &AppInfo{
		Name:        o.str("name"),
		Mode:        o.str("mode"),
		Description: o.str("description"),
		Tags:        o.strs("tags"),
		AuthorName:  o.str("author_name"),
		Raw:         o.raw(),
	}, nil
}

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

// inputField unwraps one {"text-input": {...}} entry. Dify keys each field by
// its kind rather than putting the kind inside, and an entry with no name is
// skipped rather than turned into a field called "".
func inputField(entry object) (InputField, bool) {
	for kind, config := range entry {
		cfg, ok := config.(map[string]any)
		if !ok {
			continue
		}
		c := object(cfg)
		name := c.str("variable")
		if name == "" {
			continue
		}
		f := InputField{
			Name:        name,
			Label:       c.str("label"),
			Type:        firstNonZero(c.str("type"), kind),
			Required:    c.bool("required"),
			Options:     c.strs("options"),
			Default:     c["default"],
			Description: c.str("description"),
			Hidden:      c.bool("hide"),
			Raw:         c.raw(),
		}
		if c.has("max_length") {
			n := c.int("max_length")
			f.MaxLength = &n
		}
		return f, true
	}
	return InputField{}, false
}

// Parameters is the app's declared inputs, features and limits.
func (a *App) Parameters(ctx context.Context, user string) (*AppParameters, error) {
	o, err := a.t.call(ctx, &request{method: http.MethodGet, path: "/parameters", query: params{}.set("user", a.userOr(user)).values()})
	if err != nil {
		return nil, err
	}
	p := &AppParameters{
		OpeningStatement:   o.str("opening_statement"),
		SuggestedQuestions: o.strs("suggested_questions"),
		Features:           map[string]bool{},
		FileUpload:         o.obj("file_upload").raw(),
		SystemParameters:   o.obj("system_parameters").raw(),
		Raw:                o.raw(),
	}
	for _, entry := range o.objs("user_input_form") {
		if f, ok := inputField(entry); ok {
			p.Inputs = append(p.Inputs, f)
		}
	}
	for key, value := range o {
		if m, ok := value.(map[string]any); ok {
			if _, has := m["enabled"]; has {
				p.Features[key] = object(m).bool("enabled")
			}
		}
	}
	return p, nil
}

// userOr is the user to name on a read that wants one but does not act for
// anyone in particular.
func (a *App) userOr(user string) string {
	return firstNonZero(user, firstNonZero(a.t.user, "dify-go-sdk"))
}

// Meta is the app's tool icons and other display metadata. Left a map: its
// shape is open-ended and keeps growing.
func (a *App) Meta(ctx context.Context, user string) (map[string]any, error) {
	o, err := a.t.call(ctx, &request{method: http.MethodGet, path: "/meta", query: params{}.set("user", a.userOr(user)).values()})
	return o.raw(), err
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

// Site is the WebApp settings: title, icon, theme, what visitors may do.
func (a *App) Site(ctx context.Context) (*SiteSettings, error) {
	o, err := a.t.call(ctx, &request{method: http.MethodGet, path: "/site"})
	if err != nil {
		return nil, err
	}
	return &SiteSettings{
		Title:                  o.str("title"),
		Description:            o.str("description"),
		Icon:                   o.str("icon"),
		IconType:               o.str("icon_type"),
		IconBackground:         o.str("icon_background"),
		IconURL:                o.str("icon_url"),
		DefaultLanguage:        o.str("default_language"),
		ChatColorTheme:         o.str("chat_color_theme"),
		ChatColorThemeInverted: o.bool("chat_color_theme_inverted"),
		InputPlaceholder:       o.str("input_placeholder"),
		Copyright:              o.str("copyright"),
		PrivacyPolicy:          o.str("privacy_policy"),
		CustomDisclaimer:       o.str("custom_disclaimer"),
		ShowWorkflowSteps:      o.bool("show_workflow_steps"),
		UseIconAsAnswerIcon:    o.bool("use_icon_as_answer_icon"),
		Raw:                    o.raw(),
	}, nil
}

// PageParams pages a numbered listing. Zero values take Dify's defaults.
type PageParams struct {
	Page  int
	Limit int
}

// Feedbacks is every rating left on this app's messages, end users and
// admins alike.
func (a *App) Feedbacks(ctx context.Context, p *PageParams) (*Page[map[string]any], error) {
	if p == nil {
		p = &PageParams{}
	}
	fetch := func(ctx context.Context, number int) (object, error) {
		return a.t.call(ctx, &request{method: http.MethodGet, path: "/app/feedbacks", query: params{}.setInt("page", number).setInt("limit", p.Limit).values()})
	}
	return fetchByPage(ctx, object.raw, fetch, p.Page)
}

// EndUser looks up an end user by the id other responses hand back —
// created_by on an uploaded file is one, and means nothing until resolved.
// Scoped to this app, so an id from elsewhere is not found.
func (a *App) EndUser(ctx context.Context, endUserID string) (map[string]any, error) {
	o, err := a.t.call(ctx, &request{method: http.MethodGet, path: "/end-users/" + pathEscape(endUserID)})
	return o.raw(), err
}

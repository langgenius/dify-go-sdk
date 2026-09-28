package dify

import (
	"context"
	"fmt"
	"net/http"
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
	api port

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

// BaseURL is the Service API root this client sends to.
func (a *App) BaseURL() string { return a.api.endpoint("") }

func (a *App) String() string {
	return fmt.Sprintf("dify.App(base_url=%q, api_key=%s)", a.api.endpoint(""), a.api.maskedKey())
}

// GoString keeps %#v from printing the key.
func (a *App) GoString() string { return a.String() }

// newAppOn builds an App whose every resource sends through api.
func newAppOn(api port) *App {
	return &App{
		api:         api,
		Chat:        &Chat{Messages: &Messages{api}, Conversations: &Conversations{api}},
		Workflows:   &Workflows{Runs: &WorkflowRuns{api}},
		Completions: &Completions{api},
		Files:       &Files{api},
		Annotations: &Annotations{api},
		Audio:       &Audio{api},
		Forms:       &Forms{api},
	}
}

// appPort is the port behind an App, for tests that reach the transport.
func appPort(a *App) port { return a.api }

// ServerInfo asks this client's Dify what it is. The Service API's index
// takes no credential and is the one place the running version is reliably
// reported: /openapi/v1/_version is off unless OPENAPI_ENABLED is set, and
// /console/api/version reports the latest released version rather than the
// one you are talking to.
func (a *App) ServerInfo(ctx context.Context) (*ServerInfo, error) {
	return fetchServerInfo(ctx, a.api)
}

func fetchServerInfo(ctx context.Context, api port) (*ServerInfo, error) {
	o, err := api.call(ctx, &request{method: http.MethodGet, path: "/", noAuth: true})
	if err != nil {
		return nil, err
	}
	return serverInfoFrom(o), nil
}

// Info is what this app is: its name, and the mode that decides its routes.
func (a *App) Info(ctx context.Context) (*AppInfo, error) {
	o, err := a.api.call(ctx, &request{method: http.MethodGet, path: "/info"})
	if err != nil {
		return nil, err
	}
	return appInfoFrom(o), nil
}

// Parameters is the app's declared inputs, features and limits.
func (a *App) Parameters(ctx context.Context, user string) (*AppParameters, error) {
	o, err := a.api.call(ctx, &request{method: http.MethodGet, path: "/parameters", query: params{}.set("user", a.userOr(user)).values()})
	if err != nil {
		return nil, err
	}
	return parametersFrom(o), nil
}

// userOr is the user to name on a read that wants one but does not act for
// anyone in particular.
func (a *App) userOr(user string) string {
	if who, err := a.api.who(user); err == nil {
		return who
	}
	return "dify-go-sdk"
}

// Meta is the app's tool icons and other display metadata. Left a map: its
// shape is open-ended and keeps growing.
func (a *App) Meta(ctx context.Context, user string) (map[string]any, error) {
	o, err := a.api.call(ctx, &request{method: http.MethodGet, path: "/meta", query: params{}.set("user", a.userOr(user)).values()})
	return o.raw(), err
}

// Site is the WebApp settings: title, icon, theme, what visitors may do.
func (a *App) Site(ctx context.Context) (*SiteSettings, error) {
	o, err := a.api.call(ctx, &request{method: http.MethodGet, path: "/site"})
	if err != nil {
		return nil, err
	}
	return siteFrom(o), nil
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
		return a.api.call(ctx, &request{method: http.MethodGet, path: "/app/feedbacks", query: params{}.setInt("page", number).setInt("limit", p.Limit).values()})
	}
	return fetchByPage(ctx, object.raw, fetch, p.Page)
}

// EndUser looks up an end user by the id other responses hand back —
// created_by on an uploaded file is one, and means nothing until resolved.
// Scoped to this app, so an id from elsewhere is not found.
func (a *App) EndUser(ctx context.Context, endUserID string) (map[string]any, error) {
	o, err := a.api.call(ctx, &request{method: http.MethodGet, path: "/end-users/" + pathEscape(endUserID)})
	return o.raw(), err
}

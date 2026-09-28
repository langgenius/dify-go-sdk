package usecase

import (
	"context"
	"fmt"
	"net/http"

	"github.com/langgenius/dify-go-sdk/internal/codec"
	"github.com/langgenius/dify-go-sdk/internal/entity"
	"github.com/langgenius/dify-go-sdk/internal/kernel"
	"github.com/langgenius/dify-go-sdk/internal/port"
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
	api port.Port

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
func (a *App) BaseURL() string { return a.api.Endpoint("") }

func (a *App) String() string {
	return fmt.Sprintf("dify.App(base_url=%q, api_key=%s)", a.api.Endpoint(""), a.api.MaskedKey())
}

// GoString keeps %#v from printing the key.
func (a *App) GoString() string { return a.String() }

// NewAppOn builds an App whose every resource sends through api.
func NewAppOn(api port.Port) *App {
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

// AppPort is the port behind an App, for tests that reach the transport.
func AppPort(a *App) port.Port { return a.api }

// ServerInfo asks this client's Dify what it is. The Service API's index
// takes no credential and is the one place the running version is reliably
// reported: /openapi/v1/_version is off unless OPENAPI_ENABLED is set, and
// /console/api/version reports the latest released version rather than the
// one you are talking to.
func (a *App) ServerInfo(ctx context.Context) (*entity.ServerInfo, error) {
	return FetchServerInfo(ctx, a.api)
}

func FetchServerInfo(ctx context.Context, api port.Port) (*entity.ServerInfo, error) {
	o, err := api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/", NoAuth: true})
	if err != nil {
		return nil, err
	}
	return codec.ServerInfoFrom(o), nil
}

// Info is what this app is: its name, and the mode that decides its routes.
func (a *App) Info(ctx context.Context) (*entity.AppInfo, error) {
	o, err := a.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/info"})
	if err != nil {
		return nil, err
	}
	return codec.AppInfoFrom(o), nil
}

// Parameters is the app's declared inputs, features and limits.
func (a *App) Parameters(ctx context.Context, user string) (*entity.AppParameters, error) {
	o, err := a.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/parameters", Query: port.Params{}.Set("user", a.userOr(user)).Values()})
	if err != nil {
		return nil, err
	}
	return codec.ParametersFrom(o), nil
}

// userOr is the user to name on a read that wants one but does not act for
// anyone in particular.
func (a *App) userOr(user string) string {
	if who, err := a.api.Who(user); err == nil {
		return who
	}
	return "dify-go-sdk"
}

// Meta is the app's tool icons and other display metadata. Left a map: its
// shape is open-ended and keeps growing.
func (a *App) Meta(ctx context.Context, user string) (map[string]any, error) {
	o, err := a.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/meta", Query: port.Params{}.Set("user", a.userOr(user)).Values()})
	return o.Raw(), err
}

// Site is the WebApp settings: title, icon, theme, what visitors may do.
func (a *App) Site(ctx context.Context) (*entity.SiteSettings, error) {
	o, err := a.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/site"})
	if err != nil {
		return nil, err
	}
	return codec.SiteFrom(o), nil
}

// PageParams pages a numbered listing. Zero values take Dify's defaults.
type PageParams struct {
	Page  int
	Limit int
}

// Feedbacks is every rating left on this app's messages, end users and
// admins alike.
func (a *App) Feedbacks(ctx context.Context, p *PageParams) (*codec.Page[map[string]any], error) {
	if p == nil {
		p = &PageParams{}
	}
	fetch := func(ctx context.Context, number int) (kernel.Object, error) {
		return a.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/app/feedbacks", Query: port.Params{}.SetInt("page", number).SetInt("limit", p.Limit).Values()})
	}
	return codec.FetchByPage(ctx, kernel.Object.Raw, fetch, p.Page)
}

// EndUser looks up an end user by the id other responses hand back —
// created_by on an uploaded file is one, and means nothing until resolved.
// Scoped to this app, so an id from elsewhere is not found.
func (a *App) EndUser(ctx context.Context, endUserID string) (map[string]any, error) {
	o, err := a.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: "/end-users/" + port.PathEscape(endUserID)})
	return o.Raw(), err
}

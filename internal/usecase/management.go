package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/langgenius/dify-go-sdk/internal/codec"
	"github.com/langgenius/dify-go-sdk/internal/entity"
	"github.com/langgenius/dify-go-sdk/internal/kernel"
	"github.com/langgenius/dify-go-sdk/internal/port"
)

// Management is the workspace, from an account's side: creating, publishing
// and deleting apps, minting their keys, and what the workspace has installed.
//
// It talks to the console API, which authenticates as an account rather than
// as an app — a credential to the whole account, since Dify has nothing
// narrower. That is why it is a client of its own and not more fields on App:
//
//	m, err := dify.LoginManagement(ctx, email, password, dify.WithHost("http://localhost"))
//	d, err := m.Apps.Deploy(ctx, dsl, nil)
//	if err := d.Err(dify.StageRunnable); err != nil { ... }
//	app, err := m.AppClient(d.APIKey, "alice")
//
// A Management is safe for concurrent use. A session from LoginManagement is
// renewed when it expires; one from tokens is not, since a login is what
// hands out the refresh token.
type Management struct {
	api port.Port
	// openApp builds an App on the same Dify, keyed with a Service-API key.
	// Wired by the root, which is the one place that knows how.
	openApp func(apiKey, user string) (*App, error)

	// Apps are the workspace's apps, and the steps from a DSL to a run.
	Apps *Apps
	// Agents are the workspace's Agents, which Dify keeps off the app list.
	Agents *Agents
	// Pipelines are the knowledge pipelines, and the knowledge bases they
	// fill.
	Pipelines *Pipelines
	// Models are what the workspace can call, and whether the credentials
	// for them are in place.
	Models *WorkspaceModels
	// Tools are the installed tool providers and plugins.
	Tools *Tools
	// Skills are the workspace's agent skills.
	Skills *Skills
	// DatasetKeys are the workspace's knowledge-base API keys — what
	// dify.NewKnowledge takes.
	DatasetKeys *DatasetKeys
}

// NewManagementOn builds a Management whose every resource sends through api.
func NewManagementOn(api port.Port, openApp func(apiKey, user string) (*App, error)) *Management {
	m := &Management{api: api, openApp: openApp}
	m.Agents = &Agents{api: api}
	m.Apps = &Apps{api: api, m: m, Keys: &AppKeys{api: api}, Triggers: &Triggers{api: api}}
	m.Pipelines = &Pipelines{api: api}
	m.Models = &WorkspaceModels{api: api}
	m.Tools = &Tools{api: api}
	m.Skills = &Skills{api: api}
	m.DatasetKeys = &DatasetKeys{api: api}
	return m
}

// ManagementPort is the port behind a Management, for tests that reach the
// transport.
func ManagementPort(m *Management) port.Port { return m.api }

// BaseURL is the console API root this client sends to.
func (m *Management) BaseURL() string { return m.api.Endpoint("") }

func (m *Management) String() string {
	return fmt.Sprintf("dify.Management(base_url=%q, token=%s)", m.api.Endpoint(""), m.api.MaskedKey())
}

// GoString keeps %#v from printing the token.
func (m *Management) GoString() string { return m.String() }

// AppClient is an App on the same Dify, keyed with a Service-API key — the one
// a deploy minted, most often. user is the default end-user identifier.
func (m *Management) AppClient(apiKey, user string) (*App, error) {
	if apiKey == "" {
		return nil, kernel.ArgError("no Service-API key. Apps.Keys.Create mints one, and Apps.Deploy mints one unless told not to")
	}
	return m.openApp(apiKey, user)
}

// unanswered is a request that went out and got no answer — so what Dify did
// with it is unknown. A request that failed before it was written is not
// that: Dify never saw it.
func unanswered(err error) bool {
	var te *kernel.TransportError
	if errors.As(err, &te) {
		return te.Sent
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// importAnswer reads what an import call came to. A failed import answers 400
// and still carries its status, which is a state to report rather than an
// error to raise over; a request that got no answer is reported as unknown.
func importAnswer(o kernel.Object, err error) (ans *codec.ImportAnswer, indeterminate bool, failure error) {
	if err == nil {
		return codec.ImportAnswerFrom(o), false, nil
	}
	var apiErr *kernel.APIError
	if errors.As(err, &apiErr) {
		if refused, ok := codec.ImportRefusal(apiErr.Body); ok {
			return refused, false, nil
		}
	}
	return nil, unanswered(err), err
}

// heldReason says why an import is not done, when Dify held it or refused it.
func heldReason(a *codec.ImportAnswer, confirm string) string {
	if a.Held() {
		return fmt.Sprintf("Dify wants this import confirmed: the document is DSL %s and the server is on %s. Call %s once you are satisfied the difference is safe",
			kernel.FirstNonZero(a.ImportedDSLVersion, "unknown"), kernel.FirstNonZero(a.CurrentDSLVersion, "unknown"), confirm)
	}
	return kernel.FirstNonZero(a.Error, fmt.Sprintf("Dify reported the import as %q", a.Status))
}

// isUUID is whether s is spelled like the ids Dify gives apps and knowledge
// bases, which is what decides between looking one up and searching by name.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				return false
			}
		}
	}
	return true
}

// deploymentFromImport is what an app import came to. appID is the app it was
// asked to overwrite, if any.
func deploymentFromImport(a *codec.ImportAnswer, appID string) entity.Deployment {
	d := entity.Deployment{
		ImportID:           a.ID,
		ImportedDSLVersion: a.ImportedDSLVersion,
		CurrentDSLVersion:  a.CurrentDSLVersion,
		Warnings:           a.Warnings,
		Raw:                a.Raw,
	}
	if !a.Succeeded() {
		// An overwrite that failed names the app it was aimed at; the app is
		// unchanged, and reporting it as imported would carry on to publish
		// whatever draft was already there.
		d.AppID = appID
		d.NeedsConfirmation = a.Held()
		d.Error = heldReason(a, "Apps.Confirm(ctx, held)")
		return d
	}
	d.AppID = kernel.FirstNonZero(a.AppID, appID)
	if d.AppID == "" {
		d.Error = "Dify accepted the import but returned no app id"
		return d
	}
	d.Imported = true
	d.AppMode = a.AppMode
	d.Created = appID == ""
	return d
}

package tests

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dify "github.com/langgenius/dify-go-sdk"
	"github.com/langgenius/dify-go-sdk/internal/usecase"
)

// routes answers a fake console by "METHOD /path", with the /console/api
// prefix taken off. A route nobody registered is a 404, the way Dify answers
// one.
type routes map[string]func(w http.ResponseWriter, r *http.Request)

func (rs routes) serve(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/console/api")
	if h, ok := rs[r.Method+" "+path]; ok {
		h(w, r)
		return
	}
	writeJSON(w, 404, map[string]any{"code": "not_found", "message": "no route " + r.Method + " " + path, "status": 404})
}

func answer(status int, v any) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) { writeJSON(w, status, v) }
}

func noContent(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }

// management builds a console client pointed at the fake, holding a session
// given as tokens, with retries that do not wait.
func (f *fakeDify) management(t *testing.T, opts ...dify.Option) *dify.Management {
	t.Helper()
	base := []dify.Option{dify.WithHost(f.URL), dify.WithConsoleToken("console-access-token-0001"), dify.WithCSRFToken("csrf-token-0001")}
	m, err := dify.NewManagement(append(base, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	wire(usecase.ManagementPort(m)).Sleep = func(ctx context.Context, d time.Duration) error { return ctx.Err() }
	return m
}

const appUUID = "3f1c2b8e-1d2a-4c5b-9e8f-0a1b2c3d4e5f"

func TestAConsoleRequestCarriesTheCSRFTokenOnReadsToo(t *testing.T) {
	// Dify 1.17 checks the CSRF token on every method but OPTIONS, and it has
	// to arrive twice — as a cookie and as a header that match. A client that
	// sent it on writes only would find every listing refused.
	f := newConsole(t, routes{"GET /apps": answer(200, map[string]any{"data": []any{}, "has_more": false})})
	if _, err := f.management(t).Apps.List(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	h := f.last(t).Header
	if h.Get("X-CSRF-Token") != "csrf-token-0001" {
		t.Errorf("X-CSRF-Token = %q", h.Get("X-CSRF-Token"))
	}
	cookie := h.Get("Cookie")
	for _, want := range []string{"csrf_token=csrf-token-0001", "access_token=console-access-token-0001"} {
		if !strings.Contains(cookie, want) {
			t.Errorf("cookie %q lacks %q", cookie, want)
		}
	}
	if h.Get("Authorization") != "Bearer console-access-token-0001" {
		t.Errorf("an older Dify reads the bearer header; got %q", h.Get("Authorization"))
	}
}

func TestATokenFromTheEnvironmentIsSentUnderBothCookieSpellings(t *testing.T) {
	// Behind https with no cookie domain, Dify names its cookies __Host-…
	// and reads only that name. Which one a given Dify reads cannot be told
	// from here, so a token handed in is sent under both.
	f := newConsole(t, routes{"GET /apps": answer(200, map[string]any{"data": []any{}})})
	if _, err := f.management(t).Apps.List(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	cookie := f.last(t).Header.Get("Cookie")
	if !strings.Contains(cookie, "__Host-csrf_token=csrf-token-0001") || !strings.Contains(cookie, "__Host-access_token=") {
		t.Errorf("the __Host- spelling is missing from %q", cookie)
	}
}

func TestLoggingInSendsTheirPasswordBase64AndKeepsTheCookiesItWasGiven(t *testing.T) {
	var loginBody map[string]any
	f := newConsole(t, routes{
		"POST /login": func(w http.ResponseWriter, r *http.Request) {
			// Behind https Dify prefixes the names; the client must send
			// back exactly what it was given.
			http.SetCookie(w, &http.Cookie{Name: "__Host-access_token", Value: "from-login"})
			http.SetCookie(w, &http.Cookie{Name: "__Host-csrf_token", Value: "csrf-from-login"})
			http.SetCookie(w, &http.Cookie{Name: "__Host-refresh_token", Value: "refresh-from-login"})
			writeJSON(w, 200, map[string]any{"result": "success", "data": nil})
		},
		"GET /apps": answer(200, map[string]any{"data": []any{}}),
	})
	m, err := dify.LoginManagement(context.Background(), "ops@example.com", "hunter2", dify.WithHost(f.URL))
	if err != nil {
		t.Fatal(err)
	}
	loginBody = f.seen()[0].Body
	if loginBody["password"] != base64.StdEncoding.EncodeToString([]byte("hunter2")) {
		t.Errorf("Dify base64-decodes the password field; sent %v", loginBody["password"])
	}
	if _, ok := loginBody["language"]; ok {
		t.Error("LoginPayload has no language field; sending one is sending nothing")
	}
	if _, err := m.Apps.List(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	h := f.last(t).Header
	if h.Get("Cookie") != "__Host-access_token=from-login; __Host-csrf_token=csrf-from-login" || h.Get("X-CSRF-Token") != "csrf-from-login" {
		t.Errorf("cookie %q, csrf %q", h.Get("Cookie"), h.Get("X-CSRF-Token"))
	}
	if strings.Contains(m.String(), "from-login") {
		t.Errorf("printing the client printed the token: %s", m)
	}
}

func TestALoginThatFindsNoWorkspaceIsRefusedWithDifysReason(t *testing.T) {
	// Dify answers 200 here, with result "fail" and the reason as a string.
	f := newConsole(t, routes{"POST /login": answer(200, map[string]any{"result": "fail", "data": "workspace not found, please contact system admin"})})
	_, err := dify.LoginManagement(context.Background(), "ops@example.com", "pw", dify.WithHost(f.URL))
	if !errors.Is(err, dify.ErrAuthentication) || !strings.Contains(err.Error(), "workspace not found") {
		t.Errorf("got %v", err)
	}
}

func TestAnExpiredSessionIsRenewedOnceAndTheRequestSentAgain(t *testing.T) {
	var refreshed atomic.Int32
	f := newConsole(t, routes{
		"POST /login": func(w http.ResponseWriter, r *http.Request) {
			http.SetCookie(w, &http.Cookie{Name: "access_token", Value: "old"})
			http.SetCookie(w, &http.Cookie{Name: "csrf_token", Value: "old-csrf"})
			http.SetCookie(w, &http.Cookie{Name: "refresh_token", Value: "refresh-1"})
			writeJSON(w, 200, map[string]any{"result": "success"})
		},
		"POST /refresh-token": func(w http.ResponseWriter, r *http.Request) {
			refreshed.Add(1)
			if c, err := r.Cookie("refresh_token"); err != nil || c.Value != "refresh-1" {
				writeJSON(w, 401, map[string]any{"result": "fail", "message": "No refresh token provided"})
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "access_token", Value: "new"})
			http.SetCookie(w, &http.Cookie{Name: "csrf_token", Value: "new-csrf"})
			http.SetCookie(w, &http.Cookie{Name: "refresh_token", Value: "refresh-2"})
			writeJSON(w, 200, map[string]any{"result": "success"})
		},
		"GET /apps": func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-CSRF-Token") != "new-csrf" {
				writeJSON(w, 401, map[string]any{"code": "unauthorized", "message": "Token has expired.", "status": 401})
				return
			}
			writeJSON(w, 200, map[string]any{"data": []any{}})
		},
	})
	m, err := dify.LoginManagement(context.Background(), "ops@example.com", "pw", dify.WithHost(f.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Apps.List(context.Background(), nil); err != nil {
		t.Fatalf("the renewed session should have been used: %v", err)
	}
	if refreshed.Load() != 1 {
		t.Errorf("refreshed %d times, want once", refreshed.Load())
	}
}

func TestASessionThatStaysRefusedAfterRenewingIsNotRenewedForever(t *testing.T) {
	var refreshed, listed atomic.Int32
	f := newConsole(t, routes{
		"POST /login": func(w http.ResponseWriter, r *http.Request) {
			http.SetCookie(w, &http.Cookie{Name: "access_token", Value: "a1"})
			http.SetCookie(w, &http.Cookie{Name: "refresh_token", Value: "r1"})
			writeJSON(w, 200, map[string]any{"result": "success"})
		},
		"POST /refresh-token": func(w http.ResponseWriter, r *http.Request) {
			refreshed.Add(1)
			http.SetCookie(w, &http.Cookie{Name: "access_token", Value: fmt.Sprintf("a%d", refreshed.Load()+1)})
			writeJSON(w, 200, map[string]any{"result": "success"})
		},
		"GET /apps": func(w http.ResponseWriter, r *http.Request) {
			listed.Add(1)
			writeJSON(w, 401, map[string]any{"code": "unauthorized", "message": "Invalid token.", "status": 401})
		},
	})
	m, err := dify.LoginManagement(context.Background(), "ops@example.com", "pw", dify.WithHost(f.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Apps.List(context.Background(), nil)
	if !errors.Is(err, dify.ErrAuthentication) {
		t.Fatalf("want the 401, got %v", err)
	}
	if refreshed.Load() != 1 || listed.Load() != 2 {
		t.Errorf("refreshed %d, listed %d; want 1 and 2", refreshed.Load(), listed.Load())
	}
	if !strings.Contains(err.Error(), "Invalid token.") || !strings.Contains(err.Error(), "LoginManagement") {
		t.Errorf("the error should keep Dify's words and name the fix: %v", err)
	}
}

func TestAMissingCSRFTokenIsExplainedRatherThanReportedAsABadToken(t *testing.T) {
	// A flat 401 on a token that reads fine elsewhere sends people to mint a
	// new token, which does not help: what is missing is the second one.
	f := newConsole(t, routes{"GET /apps": answer(401, map[string]any{"code": "unauthorized", "message": "CSRF token is missing or invalid.", "status": 401})})
	m, err := dify.NewManagement(dify.WithHost(f.URL), dify.WithConsoleToken("console-access-token-0001"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Apps.List(context.Background(), nil)
	if !strings.Contains(err.Error(), dify.EnvConsoleCSRFToken) || !strings.Contains(err.Error(), "reads included") {
		t.Errorf("the error should say what is missing: %v", err)
	}
}

func TestCredentialsMeantForAnotherClientAreRefusedBeforeSending(t *testing.T) {
	cases := map[string]func() error{
		"an openapi bearer": func() error {
			_, err := dify.NewManagement(dify.WithHost("http://localhost"), dify.WithConsoleToken("dfoa_abcdef"))
			return err
		},
		"a Service API root": func() error {
			_, err := dify.NewManagement(dify.WithBaseURL("http://localhost/v1"), dify.WithConsoleToken("t"))
			return err
		},
		"an app key": func() error {
			_, err := dify.NewManagement(dify.WithHost("http://localhost"), dify.WithAPIKey("app-x"))
			return err
		},
		"a console token on an app": func() error {
			_, err := dify.NewApp(dify.WithAPIKey("app-x"), dify.WithConsoleToken("t"))
			return err
		},
	}
	for name, build := range cases {
		if err := build(); !errors.Is(err, dify.ErrValidation) {
			t.Errorf("%s: want an argument error, got %v", name, err)
		}
	}
	t.Setenv(dify.EnvConsoleToken, "")
	if _, err := dify.NewManagement(dify.WithHost("http://localhost")); err == nil || !strings.Contains(err.Error(), "LoginManagement") {
		t.Errorf("no session should name how to get one: %v", err)
	}
}

func TestADeployImportsPublishesAndMintsAKeyAsSeparateFacts(t *testing.T) {
	f := newConsole(t, routes{
		"POST /apps/imports":                           answer(200, map[string]any{"id": "imp-1", "status": "completed", "app_id": appUUID, "app_mode": "workflow"}),
		"POST /apps/" + appUUID + "/workflows/publish": answer(200, map[string]any{"result": "success", "created_at": 1}),
		"POST /apps/" + appUUID + "/api-keys":          answer(201, map[string]any{"id": "k1", "token": "app-minted-secret-1234", "type": "app"}),
	})
	d, err := f.management(t).Apps.Deploy(context.Background(), "app: {}", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Imported || !d.Published || d.APIKey != "app-minted-secret-1234" || !d.Created || d.AppID != appUUID {
		t.Errorf("deployment %+v", d)
	}
	if d.Stage() != dify.StageRunnable || d.Err(dify.StageRunnable) != nil {
		t.Errorf("stage %s, err %v", d.Stage(), d.Err(dify.StageRunnable))
	}
	if body := f.seen()[0].Body; body["mode"] != "yaml-content" || body["yaml_content"] != "app: {}" {
		t.Errorf("import body %v", body)
	}
	for _, printed := range []string{d.String(), fmt.Sprintf("%v", d), fmt.Sprintf("%#v", d)} {
		if strings.Contains(printed, "minted-secret") {
			t.Errorf("printing a deployment printed its key: %s", printed)
		}
	}
}

func TestAnImportDifyRefusedIsAStateNotAnError(t *testing.T) {
	// Dify answers a failed import with 400 and the import's status in the
	// body. Raising over it would lose the reason Dify gave.
	f := newConsole(t, routes{"POST /apps/imports": answer(400, map[string]any{"id": "imp-1", "status": "failed", "error": "Missing app data in YAML content"})})
	d, err := f.management(t).Apps.Deploy(context.Background(), "nonsense", nil)
	if err != nil {
		t.Fatalf("a refusal is reported on the deployment: %v", err)
	}
	if d.Imported || d.Indeterminate || d.Error != "Missing app data in YAML content" {
		t.Errorf("deployment %+v", d)
	}
	stageErr := d.Err(dify.StageDrafted)
	var se *dify.StageError
	if !errors.As(stageErr, &se) || !strings.Contains(stageErr.Error(), "Nothing was created") {
		t.Errorf("got %v", stageErr)
	}
	if len(f.seen()) != 1 {
		t.Error("a refused import must not go on to publish")
	}
}

func TestAHeldImportWaitsForConfirmationUnlessTheCallerAcceptsTheVersion(t *testing.T) {
	held := answer(202, map[string]any{"id": "imp-9", "status": "pending", "imported_dsl_version": "0.9.0", "current_dsl_version": "0.7.0"})
	confirmed := answer(200, map[string]any{"id": "imp-9", "status": "completed", "app_id": appUUID, "app_mode": "workflow"})
	r := routes{
		"POST /apps/imports":                           held,
		"POST /apps/imports/imp-9/confirm":             confirmed,
		"POST /apps/" + appUUID + "/workflows/publish": answer(200, map[string]any{"result": "success"}),
		"POST /apps/" + appUUID + "/api-keys":          answer(201, map[string]any{"id": "k", "token": "app-k"}),
	}
	f := newFakeDify(t, r.serve)
	m := f.management(t)

	d, _ := m.Apps.Deploy(context.Background(), "doc", nil)
	if d.Imported || !d.NeedsConfirmation || d.ImportID != "imp-9" {
		t.Fatalf("a held import is neither done nor refused: %+v", d)
	}
	if err := d.Err(dify.StageDrafted); !strings.Contains(err.Error(), "Apps.Confirm") {
		t.Errorf("the error should say how to complete it: %v", err)
	}

	d, _ = m.Apps.Deploy(context.Background(), "doc", &dify.DeployParams{AcceptDSLVersion: true})
	if !d.Imported || !d.Published || !d.Created || d.APIKey != "app-k" {
		t.Errorf("accepted: %+v", d)
	}
}

func TestConfirmingAnOverwriteDoesNotClaimTheAppWasCreated(t *testing.T) {
	// Created is what makes deleting the app look safe. Confirming a held
	// overwrite once reported the caller's own app as one this call made.
	f := newConsole(t, routes{
		"POST /apps/imports":               answer(202, map[string]any{"id": "imp-2", "status": "pending"}),
		"POST /apps/imports/imp-2/confirm": answer(200, map[string]any{"id": "imp-2", "status": "completed", "app_id": appUUID}),
	})
	m := f.management(t)
	held, _ := m.Apps.Import(context.Background(), "doc", &dify.ImportParams{AppID: appUUID})
	done, _ := m.Apps.Confirm(context.Background(), held)
	if !done.Imported || done.Created {
		t.Errorf("an overwrite confirmed: %+v", done)
	}
}

func TestAnAgentPublishesOnTheRosterNotAsAWorkflow(t *testing.T) {
	// /workflows/publish serves workflow and advanced-chat only; an Agent is
	// published by its roster id, which the import does not report.
	f := newConsole(t, routes{
		"POST /apps/imports":                  answer(200, map[string]any{"id": "i", "status": "completed", "app_id": appUUID, "app_mode": "agent"}),
		"GET /agent":                          answer(200, map[string]any{"data": []any{map[string]any{"id": "roster-7", "app_id": appUUID, "name": "helper"}}, "has_more": false}),
		"POST /agent/roster-7/publish":        answer(200, map[string]any{"result": "success", "active_config_snapshot_id": "snap-3"}),
		"POST /apps/" + appUUID + "/api-keys": answer(201, map[string]any{"id": "k", "token": "app-k"}),
	})
	d, _ := f.management(t).Apps.Deploy(context.Background(), "agent doc", nil)
	if !d.Published || d.Version != "snap-3" || d.APIKey != "app-k" {
		t.Errorf("deployment %+v", d)
	}
	for _, rec := range f.seen() {
		if strings.HasSuffix(rec.Path, "/workflows/publish") {
			t.Error("an Agent was sent to /workflows/publish")
		}
	}
}

func TestAChatAppIsLiveOnImportAndIsNotPublished(t *testing.T) {
	f := newConsole(t, routes{
		"POST /apps/imports":                  answer(200, map[string]any{"id": "i", "status": "completed", "app_id": appUUID, "app_mode": "chat"}),
		"POST /apps/" + appUUID + "/api-keys": answer(201, map[string]any{"id": "k", "token": "app-k"}),
	})
	d, _ := f.management(t).Apps.Deploy(context.Background(), "chat doc", nil)
	if !d.Published || d.Err(dify.StageRunnable) != nil {
		t.Errorf("a chat app has no draft to publish, so it is live: %+v", d)
	}
}

func TestAFailedPublishLeavesAnImportedDraftToActOn(t *testing.T) {
	f := newConsole(t, routes{
		"POST /apps/imports":                           answer(200, map[string]any{"id": "i", "status": "completed", "app_id": appUUID, "app_mode": "workflow"}),
		"POST /apps/" + appUUID + "/workflows/publish": answer(400, map[string]any{"code": "invalid_param", "message": "No valid workflow found.", "status": 400}),
	})
	d, _ := f.management(t).Apps.Deploy(context.Background(), "doc", nil)
	if !d.Imported || d.Published || d.APIKey != "" || d.Indeterminate {
		t.Errorf("deployment %+v", d)
	}
	if d.Stage() != dify.StageDrafted {
		t.Errorf("stage %s", d.Stage())
	}
	if err := d.Err(dify.StageRunnable); !strings.Contains(err.Error(), appUUID+" exists on Dify") {
		t.Errorf("the error should say the app is there: %v", err)
	}
}

func TestAnImportWhoseAnswerNeverArrivedIsUnknownNotFailed(t *testing.T) {
	// The request was written and the connection dropped: Dify may have
	// created the app. Reporting "not imported" would send the caller to
	// deploy again, and leave a second app.
	f := newConsole(t, routes{
		"POST /apps/imports": func(w http.ResponseWriter, r *http.Request) {
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
		},
	})
	d, err := f.management(t, dify.WithMaxRetries(0)).Apps.Deploy(context.Background(), "doc", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Indeterminate || d.Imported || d.Stage() != dify.StageUnknown {
		t.Errorf("deployment %+v", d)
	}
	if err := d.Err(dify.StageDrafted); !strings.Contains(err.Error(), "check the workspace") {
		t.Errorf("got %v", err)
	}
}

func TestAnAppIsFoundByExactNameAcrossPages(t *testing.T) {
	// Dify's name filter matches a substring, so the first match on the first
	// page is not necessarily the app asked for.
	f := newConsole(t, routes{
		"GET /apps": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("page") == "2" {
				writeJSON(w, 200, map[string]any{"data": []any{map[string]any{"id": "b", "name": "support", "mode": "chat"}}, "has_more": false, "page": 2})
				return
			}
			writeJSON(w, 200, map[string]any{"data": []any{map[string]any{"id": "a", "name": "support-old", "mode": "chat"}}, "has_more": true, "page": 1})
		},
	})
	app, err := f.management(t).Apps.Retrieve(context.Background(), "support")
	if err != nil || app.ID != "b" {
		t.Errorf("got %v %v", app, err)
	}
}

func TestANameTwoAppsShareIsRefusedWithBothIDs(t *testing.T) {
	f := newConsole(t, routes{"GET /apps": answer(200, map[string]any{"data": []any{
		map[string]any{"id": "a", "name": "bot"}, map[string]any{"id": "b", "name": "bot"},
	}})})
	_, err := f.management(t).Apps.Retrieve(context.Background(), "bot")
	if err == nil || !strings.Contains(err.Error(), "a") || !strings.Contains(err.Error(), "b") {
		t.Errorf("got %v", err)
	}
}

func TestAnIDIsLookedUpRatherThanSearchedFor(t *testing.T) {
	f := newConsole(t, routes{"GET /apps/" + appUUID: answer(200, map[string]any{"id": appUUID, "name": "x", "mode": "workflow"})})
	app, err := f.management(t).Apps.Retrieve(context.Background(), appUUID)
	if err != nil || app.Mode != "workflow" || len(f.seen()) != 1 {
		t.Errorf("got %v %v after %d requests", app, err, len(f.seen()))
	}
}

func TestADraftRunsOnTheRouteItsModeIsServedOn(t *testing.T) {
	events := func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w, map[string]any{"event": "workflow_finished", "workflow_run_id": "run-1", "data": map[string]any{"id": "run-1", "status": "succeeded", "outputs": map[string]any{"a": 1}}})
	}
	f := newConsole(t, routes{
		"POST /apps/" + appUUID + "/workflows/draft/run":               events,
		"POST /apps/" + appUUID + "/advanced-chat/workflows/draft/run": events,
	})
	m := f.management(t)
	run, err := m.Apps.RunDraft(context.Background(), appUUID, map[string]any{"q": 1}, &dify.DraftRunParams{Mode: "workflow"})
	if err != nil || !run.Succeeded() {
		t.Fatalf("got %v %v", run, err)
	}
	if _, err := m.Apps.RunDraft(context.Background(), appUUID, nil, &dify.DraftRunParams{Mode: "advanced-chat"}); !errors.Is(err, dify.ErrValidation) {
		t.Errorf("a chatflow draft without a query: %v", err)
	}
	if _, err := m.Apps.RunDraft(context.Background(), appUUID, nil, &dify.DraftRunParams{Mode: "advanced-chat", Query: "hi"}); err != nil {
		t.Fatal(err)
	}
	if got := f.last(t); !strings.Contains(got.Path, "/advanced-chat/") || got.Body["query"] != "hi" {
		t.Errorf("sent %s %v", got.Path, got.Body)
	}
	if _, err := m.Apps.RunDraft(context.Background(), appUUID, nil, &dify.DraftRunParams{Mode: "chat"}); !errors.Is(err, dify.ErrValidation) {
		t.Errorf("a chat app has no draft: %v", err)
	}
}

func TestTheOnlyPipelinesListedAreTheBasesWithOneBehindThem(t *testing.T) {
	f := newConsole(t, routes{
		"GET /datasets": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("page") == "2" {
				writeJSON(w, 200, map[string]any{"data": []any{map[string]any{"id": "ds-2", "name": "docs 1", "pipeline_id": "pl-2", "is_published": true}}, "has_more": false})
				return
			}
			writeJSON(w, 200, map[string]any{"data": []any{map[string]any{"id": "ds-1", "name": "by hand"}}, "has_more": true})
		},
	})
	pipelines, err := f.management(t).Pipelines.List(context.Background())
	if err != nil || len(pipelines) != 1 || pipelines[0].ID != "pl-2" || pipelines[0].DatasetID != "ds-2" || !pipelines[0].Published {
		t.Errorf("got %v %v", pipelines, err)
	}
}

func TestAPipelineDeployReportsBothIDs(t *testing.T) {
	f := newConsole(t, routes{
		"POST /rag/pipelines/imports":                answer(200, map[string]any{"id": "i", "status": "completed", "pipeline_id": "pl-1", "dataset_id": "ds-1"}),
		"POST /rag/pipelines/pl-1/workflows/publish": answer(200, map[string]any{"result": "success"}),
		"DELETE /datasets/ds-1":                      noContent,
	})
	m := f.management(t)
	d, err := m.Pipelines.Deploy(context.Background(), "kind: rag_pipeline", nil)
	if err != nil || !d.Published || d.PipelineID != "pl-1" || d.DatasetID != "ds-1" {
		t.Fatalf("got %+v %v", d, err)
	}
	if _, ok := f.seen()[0].Body["name"]; ok {
		t.Error("Dify never reads a pipeline import's name; sending one is sending nothing")
	}
	if err := m.Pipelines.Delete(context.Background(), d.DatasetID); err != nil {
		t.Fatal(err)
	}
}

func TestASkillIsPublishedAfterImportUnlessItIsADraft(t *testing.T) {
	f := newConsole(t, routes{
		"POST /workspaces/current/skills/import":       answer(201, map[string]any{"id": "sk-1", "name": "tidy"}),
		"POST /workspaces/current/skills/sk-1/publish": answer(200, map[string]any{"version_number": 1}),
		"GET /workspaces/current/skills":               answer(200, map[string]any{"data": []any{map[string]any{"id": "sk-1", "name": "tidy", "latest_published_version_number": 1}}, "has_more": false}),
		"DELETE /workspaces/current/skills/sk-1":       answer(200, map[string]any{"id": "sk-1", "deleted": true}),
	})
	m := f.management(t)
	skill, err := m.Skills.Import(context.Background(), dify.FileFromReader("tidy", strings.NewReader("zip")), nil)
	if err != nil || !skill.Published() {
		t.Fatalf("got %v %v", skill, err)
	}
	if rec := f.seen()[0]; !strings.Contains(string(rec.Raw), `filename="tidy.zip"`) {
		t.Errorf("the package should upload as a zip: %s", rec.Raw)
	}
	if rec := f.seen()[1]; rec.Body == nil {
		t.Error("publish reads its body as JSON and refuses none; an empty note still sends {}")
	}
	if err := m.Skills.Delete(context.Background(), "sk-1", "Tidy"); err != nil {
		t.Fatal(err)
	}
	if f.last(t).Body["confirmation_name"] != "Tidy" {
		t.Errorf("delete body %v", f.last(t).Body)
	}
}

func TestADatasetKeyCanBeLimitedToSomeKnowledgeBases(t *testing.T) {
	f := newConsole(t, routes{"POST /datasets/api-keys": answer(200, map[string]any{"id": "k", "token": "dataset-abc", "type": "dataset"})})
	m := f.management(t)
	key, err := m.DatasetKeys.Create(context.Background(), "ds-1")
	if err != nil || key.Token != "dataset-abc" {
		t.Fatalf("got %v %v", key, err)
	}
	if ids, _ := f.last(t).Body["dataset_ids"].([]any); len(ids) != 1 || ids[0] != "ds-1" {
		t.Errorf("body %v", f.last(t).Body)
	}
	if strings.Contains(fmt.Sprint(key), "abc") || strings.Contains(fmt.Sprintf("%#v", key), "abc") {
		t.Error("printing a key printed its token")
	}
}

func TestADeployedAppIsCalledOnTheSameDifysServiceAPI(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"name": "x", "mode": "workflow"})
	})
	app, err := f.management(t).AppClient("app-deployed-key-0001", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if app.BaseURL() != f.URL+"/v1" {
		t.Errorf("base %s", app.BaseURL())
	}
	if _, err := app.Info(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := f.last(t).Header
	if h.Get("Authorization") != "Bearer app-deployed-key-0001" || h.Get("Cookie") != "" {
		t.Errorf("an app call carried the console session: %v", h)
	}
}

func TestATemporaryAppThatCouldNotBePublishedIsDeletedBeforeTheErrorReturns(t *testing.T) {
	var deleted atomic.Int32
	f := newConsole(t, routes{
		"POST /apps/imports":                           answer(200, map[string]any{"id": "i", "status": "completed", "app_id": appUUID, "app_mode": "workflow"}),
		"POST /apps/" + appUUID + "/workflows/publish": answer(400, map[string]any{"message": "No valid workflow found."}),
		"DELETE /apps/" + appUUID:                      func(w http.ResponseWriter, r *http.Request) { deleted.Add(1); w.WriteHeader(204) },
	})
	_, err := f.management(t).Apps.Temporary(context.Background(), "doc", "probe")
	if err == nil || deleted.Load() != 1 {
		t.Errorf("err %v, deleted %d", err, deleted.Load())
	}
	if name, _ := f.seen()[0].Body["name"].(string); !strings.HasPrefix(name, "probe-temporary-") {
		t.Errorf("the app should be named uniquely, got %q", name)
	}
}

func TestASessionThatCannotBeRenewedSaysSoWithoutSendingAgain(t *testing.T) {
	var listed atomic.Int32
	f := newConsole(t, routes{
		"POST /login": func(w http.ResponseWriter, r *http.Request) {
			http.SetCookie(w, &http.Cookie{Name: "access_token", Value: "a1"})
			http.SetCookie(w, &http.Cookie{Name: "refresh_token", Value: "r1"})
			writeJSON(w, 200, map[string]any{"result": "success"})
		},
		"POST /refresh-token": answer(401, map[string]any{"result": "fail", "message": "Invalid refresh token"}),
		"GET /apps": func(w http.ResponseWriter, r *http.Request) {
			listed.Add(1)
			writeJSON(w, 401, map[string]any{"code": "unauthorized", "message": "Token has expired.", "status": 401})
		},
	})
	m, err := dify.LoginManagement(context.Background(), "ops@example.com", "pw", dify.WithHost(f.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Apps.List(context.Background(), nil)
	if listed.Load() != 1 {
		t.Errorf("a request certain to be refused again was sent again: %d", listed.Load())
	}
	if !strings.Contains(err.Error(), "renewing it failed too") || !strings.Contains(err.Error(), "Invalid refresh token") {
		t.Errorf("the error should say the renewal failed and why: %v", err)
	}
}

func TestAnAppDeployedToDifyCloudIsCalledOnCloudsServiceAPI(t *testing.T) {
	// Cloud's console is cloud.dify.ai and its Service API api.dify.ai, so
	// swapping the console path for /v1 sent deployed apps to a host that
	// does not serve them.
	t.Setenv(dify.EnvHost, "")
	t.Setenv(dify.EnvAPIBaseURL, "")
	m, err := dify.NewManagement(dify.WithConsoleToken("t"))
	if err != nil {
		t.Fatal(err)
	}
	app, _ := m.AppClient("app-k", "alice")
	if app.BaseURL() != dify.DefaultBaseURL {
		t.Errorf("Cloud's apps went to %s", app.BaseURL())
	}
}

func TestAServiceAPIThatIsNotAtHostV1IsFoundWhereTheEnvironmentSays(t *testing.T) {
	t.Setenv(dify.EnvAPIBaseURL, "https://api.example.com/dify/v1/")
	m, err := dify.NewManagement(dify.WithHost("https://console.example.com"), dify.WithConsoleToken("t"))
	if err != nil {
		t.Fatal(err)
	}
	app, _ := m.AppClient("app-k", "alice")
	if app.BaseURL() != "https://api.example.com/dify/v1" {
		t.Errorf("got %s", app.BaseURL())
	}
}

func TestASessionFromADifyBefore117IsRenewedThroughTheBody(t *testing.T) {
	// Before 1.17 the tokens travelled in JSON bodies, both ways. A refresh
	// that sent the token only as a cookie and read the answer only from
	// Set-Cookie failed an hour into every session.
	f := newConsole(t, routes{
		"POST /login": answer(200, map[string]any{"result": "success", "data": map[string]any{"access_token": "old", "refresh_token": "r1"}}),
		"POST /refresh-token": func(w http.ResponseWriter, r *http.Request) {
			if b := decodeBody(r); b["refresh_token"] != "r1" {
				writeJSON(w, 401, map[string]any{"message": "no refresh token in the body"})
				return
			}
			writeJSON(w, 200, map[string]any{"result": "success", "data": map[string]any{"access_token": "new", "refresh_token": "r2"}})
		},
		"GET /apps": func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer new" {
				writeJSON(w, 401, map[string]any{"message": "Token has expired."})
				return
			}
			writeJSON(w, 200, map[string]any{"data": []any{}})
		},
	})
	m, err := dify.LoginManagement(context.Background(), "ops@example.com", "pw", dify.WithHost(f.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Apps.List(context.Background(), nil); err != nil {
		t.Fatalf("the session should have been renewed through the body: %v", err)
	}
}

func TestPluginsAreWalkedPastAFullPageWhenDifyReportsNoTotal(t *testing.T) {
	// A missing total once read as zero, ending the walk after 256 plugins
	// and reporting the rest as not installed.
	f := newConsole(t, routes{
		"GET /workspaces/current/plugin/list": func(w http.ResponseWriter, r *http.Request) {
			n := 256
			if r.URL.Query().Get("page") == "2" {
				n = 1
			}
			plugins := make([]any, n)
			for i := range plugins {
				plugins[i] = map[string]any{"plugin_id": fmt.Sprintf("p%s-%d", r.URL.Query().Get("page"), i), "plugin_unique_identifier": "x"}
			}
			writeJSON(w, 200, map[string]any{"plugins": plugins})
		},
	})
	plugins, err := f.management(t).Tools.Plugins(context.Background())
	if err != nil || len(plugins) != 257 {
		t.Errorf("got %d plugins, %v", len(plugins), err)
	}
}

func TestOpeningAWorkflowThatWasNeverPublishedDoesNotReportItRunnable(t *testing.T) {
	f := newConsole(t, routes{
		"GET /apps/" + appUUID:                        answer(200, map[string]any{"id": appUUID, "name": "draft-only", "mode": "workflow"}),
		"GET /apps/" + appUUID + "/workflows/publish": func(w http.ResponseWriter, r *http.Request) { writeRaw(w, 200, "null") },
		"POST /apps/" + appUUID + "/api-keys":         answer(201, map[string]any{"id": "k", "token": "app-k"}),
	})
	app, err := f.management(t).Apps.Open(context.Background(), appUUID, "")
	if err != nil {
		t.Fatal(err)
	}
	if app.Deployment.Published || app.Deployment.Stage() != dify.StageDrafted {
		t.Errorf("a draft-only workflow read as %s", app.Deployment.Stage())
	}
	if err := app.Deployment.Err(dify.StageRunnable); err == nil {
		t.Error("a draft-only workflow cannot be run through the Service API")
	}
}

func TestLoggingOutRevokesTheSessionAndRefusesWhatFollows(t *testing.T) {
	var loggedOut atomic.Int32
	f := newConsole(t, routes{
		"POST /logout": func(w http.ResponseWriter, r *http.Request) {
			// Dify finds the account from the session, so the cookies have
			// to arrive with the logout.
			if strings.Contains(r.Header.Get("Cookie"), "access_token=console-access-token-0001") {
				loggedOut.Add(1)
			}
			writeJSON(w, 200, map[string]any{"result": "success"})
		},
	})
	m := f.management(t)
	if err := m.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if loggedOut.Load() != 1 {
		t.Error("the logout did not carry the session it was ending")
	}
	sent := len(f.seen())
	if _, err := m.Apps.List(context.Background(), nil); !errors.Is(err, dify.ErrValidation) || !strings.Contains(err.Error(), "logged out") {
		t.Errorf("a request after logging out: %v", err)
	}
	if len(f.seen()) != sent {
		t.Error("a logged-out session was sent anyway")
	}
	if access, csrf := m.SessionTokens(); access != "" || csrf != "" {
		t.Error("a logged-out session still hands out its tokens")
	}
}

func TestTheSessionTokensHandedOutAreTheRenewedOnes(t *testing.T) {
	// A token read before a renewal and passed to another process would be
	// the expired one.
	f := newConsole(t, routes{
		"POST /login": func(w http.ResponseWriter, r *http.Request) {
			http.SetCookie(w, &http.Cookie{Name: "access_token", Value: "a1"})
			http.SetCookie(w, &http.Cookie{Name: "csrf_token", Value: "c1"})
			http.SetCookie(w, &http.Cookie{Name: "refresh_token", Value: "r1"})
			writeJSON(w, 200, map[string]any{"result": "success"})
		},
		"POST /refresh-token": func(w http.ResponseWriter, r *http.Request) {
			http.SetCookie(w, &http.Cookie{Name: "access_token", Value: "a2"})
			http.SetCookie(w, &http.Cookie{Name: "csrf_token", Value: "c2"})
			writeJSON(w, 200, map[string]any{"result": "success"})
		},
	})
	m, err := dify.LoginManagement(context.Background(), "ops@example.com", "pw", dify.WithHost(f.URL))
	if err != nil {
		t.Fatal(err)
	}
	if a, c := m.SessionTokens(); a != "a1" || c != "c1" {
		t.Errorf("before: %s %s", a, c)
	}
	if err := wire(usecase.ManagementPort(m)).ForceRefresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a, c := m.SessionTokens(); a != "a2" || c != "c2" {
		t.Errorf("after: %s %s", a, c)
	}
}

func TestTagsFilterAnAppListingAsARepeatedParameter(t *testing.T) {
	// Dify reads tag_ids with getlist and checks each is a UUID, so a
	// comma-joined value is one invalid id rather than two tags.
	f := newConsole(t, routes{"GET /apps": answer(200, map[string]any{"data": []any{}})})
	_, err := f.management(t).Apps.List(context.Background(), &dify.AppListParams{TagIDs: []string{"t1", "t2"}, SortBy: "recently_created"})
	if err != nil {
		t.Fatal(err)
	}
	q := f.last(t).Query
	if len(q["tag_ids"]) != 2 || q["sort_by"][0] != "recently_created" {
		t.Errorf("query %v", q)
	}
}

func TestARenewalThatHandsBackTheSameAccessTokenIsStillARenewal(t *testing.T) {
	// Dify's access token holds the account and an expiry in whole seconds,
	// so renewing within the second of a login returns the very same token.
	// Judging a renewal by the token changing called that a failure; the live
	// harness found it on 1.17.1.
	var refreshed atomic.Int32
	f := newConsole(t, routes{
		"POST /login": func(w http.ResponseWriter, r *http.Request) {
			http.SetCookie(w, &http.Cookie{Name: "access_token", Value: "same"})
			http.SetCookie(w, &http.Cookie{Name: "refresh_token", Value: "r1"})
			writeJSON(w, 200, map[string]any{"result": "success"})
		},
		"POST /refresh-token": func(w http.ResponseWriter, r *http.Request) {
			// Dify deletes a refresh token when it rotates it.
			if c, err := r.Cookie("refresh_token"); err != nil || c.Value != fmt.Sprintf("r%d", refreshed.Load()+1) {
				writeJSON(w, 401, map[string]any{"result": "fail", "message": "Invalid refresh token"})
				return
			}
			refreshed.Add(1)
			http.SetCookie(w, &http.Cookie{Name: "access_token", Value: "same"})
			http.SetCookie(w, &http.Cookie{Name: "refresh_token", Value: fmt.Sprintf("r%d", refreshed.Load()+1)})
			writeJSON(w, 200, map[string]any{"result": "success"})
		},
	})
	m, err := dify.LoginManagement(context.Background(), "ops@example.com", "pw", dify.WithHost(f.URL))
	if err != nil {
		t.Fatal(err)
	}
	if err := wire(usecase.ManagementPort(m)).ForceRefresh(context.Background()); err != nil {
		t.Fatalf("a renewal answered with the same token: %v", err)
	}
	// And a second renewal uses the rotated refresh token, not a spent one.
	if err := wire(usecase.ManagementPort(m)).ForceRefresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if refreshed.Load() != 2 {
		t.Errorf("refreshed %d times", refreshed.Load())
	}
}

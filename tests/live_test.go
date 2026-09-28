package tests

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	dify "github.com/langgenius/dify-go-sdk"
	"github.com/langgenius/dify-go-sdk/internal/kernel"
)

// The live harness: contract tests against a running Dify, so the claims in
// this package are checked against a server rather than a mock.
//
//	set -a; . ../dify-python-sdk/.env; set +a    # DIFY_HOST, DIFY_CONSOLE_EMAIL, DIFY_CONSOLE_PASSWORD
//	go test -run Live ./...
//
// It logs in to the console, imports the fixture apps in testdata/, publishes
// them, mints their keys, and deletes all of it afterwards — including what a
// crashed earlier run left behind, found by the sdk-go-harness prefix. The
// fixture apps use template nodes, not model nodes, so a run costs nothing.
//
// It also mints a dataset key and exports it as DIFY_DATASET_API_KEY for the
// duration, so the knowledge tests that gate on it run too; the key is revoked
// at the end, because a workspace holds only ten and a leaked one fills the cap
// without the error ever saying so.

const harnessPrefix = "sdk-go-harness"

// live is what the harness set up, or why it could not.
var live struct {
	skip        string
	host        string
	workflowKey string
	chatKey     string
}

func TestMain(m *testing.M) {
	cleanup := setUpLive()
	code := m.Run()
	cleanup()
	os.Exit(code)
}

func requireLive(t *testing.T) {
	t.Helper()
	if live.skip != "" {
		t.Skip(live.skip)
	}
}

func setUpLive() func() {
	host := strings.TrimRight(os.Getenv(dify.EnvHost), "/")
	email, password := os.Getenv("DIFY_CONSOLE_EMAIL"), os.Getenv("DIFY_CONSOLE_PASSWORD")
	if host == "" || email == "" || password == "" {
		live.skip = "no Dify configured: set DIFY_HOST, DIFY_CONSOLE_EMAIL and DIFY_CONSOLE_PASSWORD"
		return func() {}
	}
	live.host = host
	c, err := consoleLogin(host, email, password)
	if err != nil {
		live.skip = "console login failed: " + err.Error()
		return func() {}
	}
	c.sweep()

	var undo []func()
	cleanup := func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
	}
	for _, fixture := range []struct {
		file string
		key  *string
	}{{"testdata/workflow.yml", &live.workflowKey}, {"testdata/chatflow.yml", &live.chatKey}} {
		appID, key, err := c.deploy(fixture.file)
		if appID != "" {
			undo = append(undo, func() { c.deleteApp(appID) })
		}
		if err != nil {
			live.skip = fmt.Sprintf("deploying %s: %v", fixture.file, err)
			return cleanup
		}
		*fixture.key = key
	}
	if os.Getenv(dify.EnvDatasetAPIKey) == "" {
		if keyID, token, err := c.mintDatasetKey(); err == nil {
			os.Setenv(dify.EnvDatasetAPIKey, token)
			undo = append(undo, func() { c.revokeDatasetKey(keyID); os.Unsetenv(dify.EnvDatasetAPIKey) })
		} else {
			fmt.Fprintln(os.Stderr, "live harness: no dataset key:", err)
		}
	}
	return cleanup
}

// console is the least of the console API the harness needs. It is test
// scaffolding, not SDK surface: managing apps needs an account session, which
// is a different credential from anything App or Knowledge holds.
type console struct {
	base  string
	token string
	csrf  string
	http  *http.Client
}

func consoleLogin(host, email, password string) (*console, error) {
	c := &console{base: host + "/console/api", http: &http.Client{Timeout: 60 * time.Second}}
	body, _ := json.Marshal(map[string]any{
		"email": email,
		// Dify base64-decodes this field: obfuscation for transport.
		"password": base64.StdEncoding.EncodeToString([]byte(password)),
		"language": "en-US",
	})
	resp, err := c.http.Post(c.base+"/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out["result"] != "success" {
		return nil, fmt.Errorf("login refused: %v", out)
	}
	// Since 1.17 the tokens are cookies, and writes need the CSRF one too.
	for _, cookie := range resp.Cookies() {
		switch cookie.Name {
		case "access_token":
			c.token = cookie.Value
		case "csrf_token":
			c.csrf = cookie.Value
		}
	}
	if c.token == "" {
		if data, ok := out["data"].(map[string]any); ok {
			c.token, _ = data["access_token"].(string)
		}
	}
	if c.token == "" {
		return nil, fmt.Errorf("login returned no access token")
	}
	return c, nil
}

func (c *console) do(method, path string, body any) (map[string]any, error) {
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	cookies := "access_token=" + c.token
	if c.csrf != "" {
		cookies += "; csrf_token=" + c.csrf
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	req.Header.Set("Cookie", cookies)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode >= 400 {
		return out, fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, raw)
	}
	return out, nil
}

// deploy imports a DSL, publishes it and mints a key — the three steps a
// definition needs before the Service API will run it. Importing writes only
// the draft, and a draft answers every run with "workflow not published".
func (c *console) deploy(file string) (appID, key string, err error) {
	yaml, err := os.ReadFile(file)
	if err != nil {
		return "", "", err
	}
	imported, err := c.do("POST", "/apps/imports", map[string]any{"mode": "yaml-content", "yaml_content": string(yaml)})
	if err != nil {
		return "", "", err
	}
	if imported["status"] == "pending" {
		if imported, err = c.do("POST", fmt.Sprintf("/apps/imports/%v/confirm", imported["id"]), nil); err != nil {
			return "", "", err
		}
	}
	appID, _ = imported["app_id"].(string)
	if appID == "" {
		return "", "", fmt.Errorf("import did not create an app: %v", imported)
	}
	if _, err := c.do("POST", "/apps/"+appID+"/workflows/publish", map[string]any{"marked_name": "", "marked_comment": ""}); err != nil {
		return appID, "", err
	}
	minted, err := c.do("POST", "/apps/"+appID+"/api-keys", nil)
	if err != nil {
		return appID, "", err
	}
	key, _ = minted["token"].(string)
	return appID, key, nil
}

func (c *console) deleteApp(appID string) {
	if _, err := c.do("DELETE", "/apps/"+appID, nil); err != nil {
		fmt.Fprintln(os.Stderr, "live harness: could not delete app", appID, err)
	}
}

// sweep deletes harness apps a crashed earlier run left behind.
func (c *console) sweep() {
	listed, err := c.do("GET", "/apps?page=1&limit=100&name="+harnessPrefix, nil)
	if err != nil {
		return
	}
	for _, item := range kernel.Object(listed).Objs("data") {
		if strings.HasPrefix(item.Str("name"), harnessPrefix) {
			c.deleteApp(item.Str("id"))
		}
	}
}

func (c *console) mintDatasetKey() (id, token string, err error) {
	minted, err := c.do("POST", "/datasets/api-keys", nil)
	if err != nil {
		return "", "", err
	}
	id, _ = minted["id"].(string)
	token, _ = minted["token"].(string)
	return id, token, nil
}

func (c *console) revokeDatasetKey(id string) {
	if _, err := c.do("DELETE", "/datasets/api-keys/"+id, nil); err != nil {
		fmt.Fprintln(os.Stderr, "live harness: could not revoke dataset key", id, err)
	}
}

func liveApp(t *testing.T, key string) *dify.App {
	t.Helper()
	requireLive(t)
	a, err := dify.NewApp(dify.WithAPIKey(key), dify.WithBaseURL(live.host+"/v1"), dify.WithUser(harnessPrefix))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

package tests

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// consoleRoutes is every console route Management sends to, written the way
// Dify's controllers register it. Two tests hold it to both sides: every
// request the console tests see must match an entry, and every entry must be
// a route and a method Dify serves. A method pointed at a path Dify never
// served passes any number of mocked tests — the Python SDK once had
// seventeen — so the table is what a mock is checked against.
var consoleRoutes = []string{
	"POST /login",
	"POST /logout",
	"POST /refresh-token",
	"GET /apps",
	"GET /apps/<uuid:app_id>",
	"DELETE /apps/<uuid:app_id>",
	"GET /apps/<uuid:app_id>/export",
	"POST /apps/imports",
	"POST /apps/imports/<string:import_id>/confirm",
	"GET /apps/<uuid:app_id>/workflows/publish",
	"POST /apps/<uuid:app_id>/workflows/publish",
	"POST /apps/<uuid:app_id>/workflows/draft/run",
	"POST /apps/<uuid:app_id>/advanced-chat/workflows/draft/run",
	"GET /apps/<uuid:resource_id>/api-keys",
	"POST /apps/<uuid:resource_id>/api-keys",
	"DELETE /apps/<uuid:resource_id>/api-keys/<uuid:api_key_id>",
	"GET /apps/<uuid:app_id>/triggers",
	"POST /apps/<uuid:app_id>/trigger-enable",
	"GET /apps/<uuid:app_id>/workflows/triggers/webhook",
	"GET /agent",
	"POST /agent/<uuid:agent_id>/publish",
	"POST /rag/pipelines/imports",
	"POST /rag/pipelines/imports/<string:import_id>/confirm",
	"POST /rag/pipelines/<uuid:pipeline_id>/workflows/publish",
	"GET /rag/pipelines/<string:pipeline_id>/exports",
	"GET /datasets",
	"DELETE /datasets/<uuid:dataset_id>",
	"GET /datasets/api-keys",
	"POST /datasets/api-keys",
	"DELETE /datasets/api-keys/<uuid:api_key_id>",
	"GET /workspaces/current/skills",
	"POST /workspaces/current/skills/import",
	"POST /workspaces/current/skills/<string:skill_id>/publish",
	"DELETE /workspaces/current/skills/<string:skill_id>",
	"GET /workspaces/current/model-providers",
	"GET /workspaces/current/models/model-types/<string:model_type>",
	"GET /workspaces/current/tool-providers",
	"GET /workspaces/current/tool-provider/builtin/<path:provider>/tools",
	"GET /workspaces/current/plugin/list",
}

// routeMatches reports whether a request path fits a route template. A
// converter matches one segment, except <path:...>, which matches the rest.
func routeMatches(template, path string) bool {
	want := strings.Split(strings.Trim(template, "/"), "/")
	got := strings.Split(strings.Trim(path, "/"), "/")
	for i, seg := range want {
		if strings.HasPrefix(seg, "<path:") {
			return len(got) > i
		}
		if i >= len(got) {
			return false
		}
		if strings.HasPrefix(seg, "<") {
			continue
		}
		if seg != got[i] {
			return false
		}
	}
	return len(want) == len(got)
}

func listedRoute(method, path string) bool {
	for _, route := range consoleRoutes {
		m, template, _ := strings.Cut(route, " ")
		if m == method && routeMatches(template, path) {
			return true
		}
	}
	return false
}

// newConsole is a fake console that fails the test when the SDK sends a
// request consoleRoutes does not list.
func newConsole(t *testing.T, rs routes) *fakeDify {
	t.Helper()
	return newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/console/api")
		if strings.HasPrefix(r.URL.Path, "/console/api") && !listedRoute(r.Method, path) {
			t.Errorf("Management sent %s %s, which consoleRoutes does not list; add it once the controller shows Dify serves it", r.Method, path)
		}
		rs.serve(w, r)
	})
}

func TestTheRouteTableMatchesWhatItMeansTo(t *testing.T) {
	cases := map[[2]string]bool{
		{"/apps/<uuid:app_id>/export", "/apps/abc/export"}: true,
		{"/apps/<uuid:app_id>", "/apps/abc/export"}:        false,
		{"/workspaces/current/tool-provider/builtin/<path:provider>/tools", "/workspaces/current/tool-provider/builtin/a/b/c/tools"}: true,
		{"/apps", "/apps/"}: true,
	}
	for c, want := range cases {
		if got := routeMatches(c[0], c[1]); got != want {
			t.Errorf("routeMatches(%q, %q) = %v", c[0], c[1], got)
		}
	}
}

// difyConsole is the console controllers of a Dify checkout beside this repo,
// or DIFY_OSS_DIR. CI has none, and skips.
func difyConsole(t *testing.T) string {
	t.Helper()
	root := os.Getenv("DIFY_OSS_DIR")
	if root == "" {
		root = "../../dify-oss"
	}
	dir := filepath.Join(root, "api", "controllers", "console")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("no Dify checkout at %s; set DIFY_OSS_DIR to check the route table against its controllers", root)
	}
	return dir
}

var (
	routeDecorator = regexp.MustCompile(`@console_ns\.route\(\s*"([^"]+)"`)
	addResource    = regexp.MustCompile(`add_resource\(\s*(\w+)\s*,\s*((?:"[^"]+"\s*,?\s*)+)`)
	classLine      = regexp.MustCompile(`^class (\w+)\(([^)]*)\)`)
	methodLine     = regexp.MustCompile(`^    def (get|post|put|patch|delete)\(`)
	quoted         = regexp.MustCompile(`"([^"]+)"`)
)

// servedRoutes reads which methods Dify serves on which paths, from the
// resource classes: a class takes the routes decorating it and those passed
// to add_resource, and serves the methods it defines or inherits from a class
// in the same file.
func servedRoutes(t *testing.T, dir string) map[string]map[string]bool {
	served := map[string]map[string]bool{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".py") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		methods := map[string]map[string]bool{}
		bases := map[string][]string{}
		routesOf := map[string][]string{}
		var pending []string
		current := ""
		for _, line := range strings.Split(string(src), "\n") {
			if m := routeDecorator.FindStringSubmatch(line); m != nil {
				pending = append(pending, m[1])
				continue
			}
			if m := classLine.FindStringSubmatch(line); m != nil {
				current = m[1]
				methods[current] = map[string]bool{}
				for _, b := range strings.Split(m[2], ",") {
					bases[current] = append(bases[current], strings.TrimSpace(b))
				}
				routesOf[current] = append(routesOf[current], pending...)
				pending = nil
				continue
			}
			if m := methodLine.FindStringSubmatch(line); m != nil && current != "" {
				methods[current][strings.ToUpper(m[1])] = true
			}
		}
		for _, m := range addResource.FindAllStringSubmatch(string(src), -1) {
			for _, q := range quoted.FindAllStringSubmatch(m[2], -1) {
				routesOf[m[1]] = append(routesOf[m[1]], q[1])
			}
		}
		var serves func(class string, seen map[string]bool) map[string]bool
		serves = func(class string, seen map[string]bool) map[string]bool {
			out := map[string]bool{}
			if seen[class] {
				return out
			}
			seen[class] = true
			for m := range methods[class] {
				out[m] = true
			}
			for _, b := range bases[class] {
				for m := range serves(b, seen) {
					out[m] = true
				}
			}
			return out
		}
		for class, paths := range routesOf {
			for _, p := range paths {
				if served[p] == nil {
					served[p] = map[string]bool{}
				}
				for m := range serves(class, map[string]bool{}) {
					served[p][m] = true
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return served
}

func TestEveryConsoleRouteManagementSendsToIsOneDifyServes(t *testing.T) {
	served := servedRoutes(t, difyConsole(t))
	var missing []string
	for _, route := range consoleRoutes {
		method, path, _ := strings.Cut(route, " ")
		if !served[path][method] {
			missing = append(missing, route)
		}
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("Dify's controllers serve no %s", m)
	}
}

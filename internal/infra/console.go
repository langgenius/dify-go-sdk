package infra

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/langgenius/dify-go-sdk/internal/kernel"
	"github.com/langgenius/dify-go-sdk/internal/port"
)

const (
	// EnvConsoleToken is a console session's access token, read by
	// NewManagement. Named apart from difyctl's DIFY_TOKEN, which holds an
	// /openapi/v1 bearer — a different credential the console refuses.
	EnvConsoleToken = "DIFY_CONSOLE_TOKEN"
	// EnvConsoleCSRFToken is the CSRF token that goes with it. Since Dify 1.17
	// every console request, reads included, needs both.
	EnvConsoleCSRFToken = "DIFY_CONSOLE_CSRF_TOKEN"
	// DefaultConsoleHost is Dify Cloud.
	DefaultConsoleHost = "https://cloud.dify.ai"
)

// Dify's cookie names, which it prefixes with __Host- when both its web and
// API URLs are https and no cookie domain is set. The server reads only the
// name it would have written.
const (
	cookieAccess  = "access_token"
	cookieCSRF    = "csrf_token"
	cookieRefresh = "refresh_token"
	hostPrefix    = "__Host-"
	csrfHeader    = "X-CSRF-Token"
)

// openAPIPrefixes are the bearers Dify mints for /openapi/v1. The console
// answers one with a flat 401 that says nothing about why.
var openAPIPrefixes = []string{"dfoa_", "dfoe_"}

// WithHost sets the Dify host, e.g. http://localhost. NewManagement talks to
// <host>/console/api; NewApp and NewKnowledge derive <host>/v1 from it unless
// WithBaseURL says otherwise. Left out, it is DIFY_HOST, then Dify Cloud.
func WithHost(host string) Option { return func(c *config) { c.host = host } }

// WithConsoleToken sets a console session's access token. Left out, it is read
// from DIFY_CONSOLE_TOKEN. dify.LoginManagement obtains one from an email and
// password instead.
func WithConsoleToken(token string) Option { return func(c *config) { c.consoleToken = &token } }

// WithCSRFToken sets the CSRF token that goes with a console access token.
// Left out, it is read from DIFY_CONSOLE_CSRF_TOKEN. A Dify before 1.17 needs
// none; since 1.17 every request is refused without it.
func WithCSRFToken(token string) Option { return func(c *config) { c.csrfToken = &token } }

// session is a console login: an access token, the CSRF token paired with
// it, and — when it came from a login — the refresh token that renews both.
//
// It is shared by every request on the client, and a refresh replaces all
// three at once, so it is guarded.
type session struct {
	mu                    sync.Mutex
	access, csrf, refresh string
	// names are the cookie names to send each token under. A login records
	// the names Dify set; a token from the environment is sent under both
	// spellings, since which one this Dify reads is not knowable from here.
	accessNames, csrfNames []string
	refreshName            string
}

// apply puts the session on a request, and returns the access token it used,
// so a refresh can tell whether someone else already renewed it.
func (s *session) apply(req *http.Request) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The bearer header is what a Dify before 1.17 reads; later ones read
	// the cookie first and fall back to it.
	req.Header.Set("Authorization", "Bearer "+s.access)
	var cookies []string
	for _, name := range s.accessNames {
		cookies = append(cookies, name+"="+s.access)
	}
	if s.csrf != "" {
		for _, name := range s.csrfNames {
			cookies = append(cookies, name+"="+s.csrf)
		}
		req.Header.Set(csrfHeader, s.csrf)
	}
	req.Header.Set("Cookie", strings.Join(cookies, "; "))
	return s.access
}

func (s *session) masked() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return kernel.MaskSecret(s.access)
}

func (s *session) canRefresh() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refresh != ""
}

// take reads a login's or a refresh's Set-Cookie headers into the session.
func (s *session) take(cookies []*http.Cookie) {
	for _, c := range cookies {
		if c.Value == "" {
			continue
		}
		switch strings.TrimPrefix(c.Name, hostPrefix) {
		case cookieAccess:
			s.access, s.accessNames = c.Value, []string{c.Name}
		case cookieCSRF:
			s.csrf, s.csrfNames = c.Value, []string{c.Name}
		case cookieRefresh:
			s.refresh, s.refreshName = c.Value, c.Name
		}
	}
}

// NewConsoleTransport builds the transport for a console session given as
// tokens, through the options or the environment.
func NewConsoleTransport(opts []Option) (*Transport, error) {
	c, err := consoleConfig(opts)
	if err != nil {
		return nil, err
	}
	var token string
	switch {
	case c.consoleToken != nil && *c.consoleToken == "":
		return nil, kernel.ArgError("the console token is empty. Pass a value, or leave WithConsoleToken out to read %s", EnvConsoleToken)
	case c.consoleToken != nil:
		token = *c.consoleToken
	default:
		token = os.Getenv(EnvConsoleToken)
	}
	if token == "" {
		return nil, kernel.ArgError("no console session. Call dify.LoginManagement(ctx, email, password), or set %s and %s", EnvConsoleToken, EnvConsoleCSRFToken)
	}
	for _, prefix := range openAPIPrefixes {
		if strings.HasPrefix(token, prefix) {
			return nil, kernel.ArgError("this looks like an /openapi/v1 OAuth bearer (%s…), the kind difyctl mints. Management speaks to /console/api, which needs a console session: set %s, or call dify.LoginManagement(ctx, email, password)", prefix, EnvConsoleToken)
		}
	}
	csrf := os.Getenv(EnvConsoleCSRFToken)
	if c.csrfToken != nil {
		csrf = *c.csrfToken
	}
	t := consoleTransport(c)
	t.session = &session{
		access:      token,
		csrf:        csrf,
		accessNames: []string{cookieAccess, hostPrefix + cookieAccess},
		csrfNames:   []string{cookieCSRF, hostPrefix + cookieCSRF},
	}
	return t, nil
}

// ConsoleLogin exchanges an account's email and password for a console
// session, and returns a transport holding it.
//
// The password goes base64-encoded because Dify decodes that field — it is
// obfuscation for transport, not encryption; HTTPS is what protects it. It is
// used for this request and not kept.
func ConsoleLogin(ctx context.Context, opts []Option, email, password string) (*Transport, error) {
	c, err := consoleConfig(opts)
	if err != nil {
		return nil, err
	}
	if email == "" || password == "" {
		return nil, kernel.ArgError("logging in needs both an email and a password")
	}
	t := consoleTransport(c)
	resp, raw, err := t.send(ctx, &port.Request{
		Method: http.MethodPost,
		Path:   "/login",
		Body:   map[string]any{"email": email, "password": base64.StdEncoding.EncodeToString([]byte(password))},
		NoAuth: true,
	})
	if err != nil {
		return nil, err
	}
	o, err := decodeObject(raw)
	if err != nil {
		return nil, err
	}
	if o.Str("result") != "success" {
		// A 200 that is not a success: an account with no workspace, most
		// often, which Dify reports as a string under data.
		detail := kernel.FirstNonZero(o.Str("data"), kernel.FirstNonZero(o.Str("message"), "no detail given"))
		return nil, fmt.Errorf("%w: console login refused: %s", kernel.ErrAuthentication, detail)
	}
	s := &session{}
	s.take(resp.Cookies())
	if s.access == "" {
		// Before 1.17 the tokens came in the body.
		data := o.Obj("data")
		s.access, s.refresh = data.Str("access_token"), data.Str("refresh_token")
		s.accessNames, s.refreshName = []string{cookieAccess}, cookieRefresh
	}
	if s.access == "" {
		return nil, fmt.Errorf("%w: console login succeeded but returned no access token, in neither a cookie nor the body", kernel.ErrAuthentication)
	}
	t.session = s
	return t, nil
}

func consoleConfig(opts []Option) (config, error) {
	c := config{timeout: DefaultTimeout, maxRetries: DefaultMaxRetries, retryDelay: DefaultRetryDelay}
	for _, opt := range opts {
		opt(&c)
	}
	if c.baseURL != "" {
		return c, kernel.ArgError("WithBaseURL sets the Service API root, and Management talks to the console under the host: pass dify.WithHost(%q) instead", strings.TrimSuffix(strings.TrimRight(c.baseURL, "/"), "/v1"))
	}
	if c.apiKey != nil || c.keyFunc != nil {
		return c, kernel.ArgError("WithAPIKey sets a Service-API key, which the console does not take. A console session comes from dify.LoginManagement, or WithConsoleToken and WithCSRFToken")
	}
	return c, nil
}

func consoleTransport(c config) *Transport {
	h := c.httpClient
	if h == nil {
		h = &http.Client{}
	}
	logger := c.logger
	if logger == nil {
		logger = newDiscardLogger()
	}
	host := ResolveHost(c.host)
	return &Transport{
		baseURL:     host + "/console/api",
		serviceBase: serviceBaseFor(host),
		http:        h,
		timeout:     c.timeout,
		maxRetries:  c.maxRetries,
		retryDelay:  c.retryDelay,
		logger:      logger,
		Sleep:       kernel.SleepCtx,
	}
}

// ResolveHost picks the Dify host: the option, DIFY_HOST, then Dify Cloud.
func ResolveHost(explicit string) string {
	if explicit != "" {
		return strings.TrimRight(explicit, "/")
	}
	if v := os.Getenv(EnvHost); v != "" {
		return strings.TrimRight(v, "/")
	}
	return DefaultConsoleHost
}

// ServiceBaseURL is the Service API root on the same Dify as this console
// transport, for handing a deployed app's key to an App client.
func (t *Transport) ServiceBaseURL() string { return t.serviceBase }

// serviceBaseFor is where the Service API sits beside a console host.
// DIFY_API_BASE_URL wins, as it does for NewApp: it is the setting for a Dify
// whose Service API is not at <host>/v1. Dify Cloud is one — its console is
// cloud.dify.ai and its Service API api.dify.ai — so replacing the console
// path would send every deployed app's calls to a host that does not serve
// them.
func serviceBaseFor(host string) string {
	if v := os.Getenv(EnvAPIBaseURL); v != "" {
		return strings.TrimRight(v, "/")
	}
	if host == DefaultConsoleHost {
		return DefaultBaseURL
	}
	return host + "/v1"
}

// refresh renews the session with its refresh token. seen is the access token
// the failed request carried: if it has changed, another request already
// renewed the session and there is nothing to do.
func (t *Transport) refresh(ctx context.Context, seen string) error {
	s := t.session
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.access != seen {
		return nil
	}
	if s.refresh == "" {
		return fmt.Errorf("no refresh token")
	}
	rctx, cancel := ctx, context.CancelFunc(func() {})
	if _, has := ctx.Deadline(); !has && t.timeout > 0 {
		rctx, cancel = context.WithTimeout(ctx, t.timeout)
	}
	defer cancel()
	// Since 1.17 Dify reads the refresh token from its cookie and nowhere
	// else; before, from the JSON body. Both are sent, since which one this
	// Dify is cannot be told from here, and each ignores the other.
	body, _ := json.Marshal(map[string]string{"refresh_token": s.refresh})
	req, err := http.NewRequestWithContext(rctx, http.MethodPost, t.baseURL+"/refresh-token", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", s.refreshName+"="+s.refresh)
	req.Header.Set("User-Agent", UserAgent())
	resp, err := t.http.Do(req)
	if err != nil {
		return err
	}
	defer drain(resp)
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("refresh answered %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	before := s.access
	s.take(resp.Cookies())
	if s.access == before {
		// Before 1.17 the renewed pair came back in the body, as a login's did.
		if o, err := decodeObject(raw); err == nil {
			data := o.Obj("data")
			if token := data.Str("access_token"); token != "" {
				s.access = token
			}
			if token := data.Str("refresh_token"); token != "" {
				s.refresh = token
			}
		}
	}
	if s.access == before {
		return fmt.Errorf("refresh answered without a new access token")
	}
	t.logger.DebugContext(ctx, "dify console session refreshed", "at", time.Now().Format(time.RFC3339))
	return nil
}

// explainConsole rewords a console 401 to name the fix. Dify's own words are
// kept in the message, after it.
func explainConsole(e *kernel.APIError, refreshErr error) {
	said := kernel.FirstNonZero(e.Message, "401")
	if strings.Contains(strings.ToLower(said), "csrf") {
		e.Message = fmt.Sprintf("Dify refused the console session's CSRF token (%s). Since 1.17 every console request, reads included, pairs the access token with a CSRF token: use dify.LoginManagement, which collects both, or set %s beside %s — the browser keeps it as the csrf_token cookie", said, EnvConsoleCSRFToken, EnvConsoleToken)
		return
	}
	renew := "log in again with dify.LoginManagement, or set a fresh " + EnvConsoleToken
	if refreshErr != nil {
		renew = fmt.Sprintf("renewing it failed too (%v); %s", refreshErr, renew)
	}
	e.Message = fmt.Sprintf("Dify rejected the console session (%s). A session lasts an hour by default; %s", said, renew)
}

// ServiceTransportFor is a Service-API transport on the same Dify as this
// console one, keyed with an app's key: the same HTTP client, timeout,
// retries and logger, so an app a deploy hands back behaves like the client
// that deployed it.
func (t *Transport) ServiceTransportFor(apiKey, user string) *Transport {
	return &Transport{
		Key:        secretKey{Static: apiKey},
		baseURL:    t.ServiceBaseURL(),
		http:       t.http,
		timeout:    t.timeout,
		maxRetries: t.maxRetries,
		retryDelay: t.retryDelay,
		user:       user,
		logger:     t.logger,
		Sleep:      t.Sleep,
	}
}

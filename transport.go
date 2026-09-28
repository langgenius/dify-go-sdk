package dify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultTimeout bounds a request whose context carries no deadline. A
	// context with a deadline of its own replaces it, which is how one long
	// workflow run gets twenty minutes without every call getting them.
	DefaultTimeout = 60 * time.Second
	// DefaultMaxRetries is how many times a request is repeated after the
	// first attempt, when repeating it is safe.
	DefaultMaxRetries = 3
	// DefaultRetryDelay is the first backoff; each retry doubles it.
	DefaultRetryDelay = time.Second
	// MaxRateLimitWait is the longest this client sits out a 429, however long
	// Dify asks for. A server under maintenance can answer Retry-After: 3600,
	// and a call that blocks for an hour is indistinguishable from one that
	// hung — so beyond this the 429 is returned, with RetryAfter set.
	MaxRateLimitWait = 60 * time.Second
)

// Option configures a client. The same options serve App and Knowledge.
type Option func(*config)

type config struct {
	apiKey     *string
	keyFunc    KeyFunc
	baseURL    string
	httpClient *http.Client
	timeout    time.Duration
	maxRetries int
	retryDelay time.Duration
	user       string
	logger     *slog.Logger
}

// WithAPIKey sets the key. Left out, it is read from the environment.
func WithAPIKey(key string) Option { return func(c *config) { c.apiKey = &key } }

// WithAPIKeyFunc fetches the key per request instead of holding it, so a
// rotating or short-lived key works without rebuilding the client and no
// long-lived secret sits on it.
func WithAPIKeyFunc(f KeyFunc) Option { return func(c *config) { c.keyFunc = f } }

// WithBaseURL sets the Service API root, e.g. http://localhost/v1. Left out,
// it is DIFY_API_BASE_URL, then <DIFY_HOST>/v1, then Dify Cloud.
func WithBaseURL(u string) Option { return func(c *config) { c.baseURL = u } }

// WithHTTPClient sends through your own client — for a proxy, a corporate TLS
// bundle, a shared connection pool, or a RoundTripper that never leaves the
// test process.
func WithHTTPClient(h *http.Client) Option { return func(c *config) { c.httpClient = h } }

// WithTimeout changes DefaultTimeout. Zero means no timeout beyond the
// context's. On a stream it bounds the silence between two events rather than
// the whole stream, since a run that keeps reporting progress is not hung.
func WithTimeout(d time.Duration) Option { return func(c *config) { c.timeout = d } }

// WithMaxRetries changes DefaultMaxRetries. Zero disables retrying.
func WithMaxRetries(n int) Option { return func(c *config) { c.maxRetries = n } }

// WithRetryDelay changes DefaultRetryDelay.
func WithRetryDelay(d time.Duration) Option { return func(c *config) { c.retryDelay = d } }

// WithUser sets the end-user identifier sent when a call does not name one.
// Dify wants one on nearly every app request; setting it here is the usual
// case, passing it per call is for a server acting for many users.
func WithUser(user string) Option { return func(c *config) { c.user = user } }

// WithLogger logs each request and response at debug level, and retries at
// warn. Headers are never logged, so the key does not end up in a log.
func WithLogger(l *slog.Logger) Option { return func(c *config) { c.logger = l } }

// transport holds the connection, the credential, retries and error mapping,
// and nothing about what Dify can do. The resources name the operations.
type transport struct {
	key        secretKey
	baseURL    string
	http       *http.Client
	timeout    time.Duration
	maxRetries int
	retryDelay time.Duration
	user       string
	logger     *slog.Logger
	// sleep waits between attempts. A field so tests do not wait.
	sleep func(ctx context.Context, d time.Duration) error
}

func newTransport(opts []Option, keyEnv ...string) (*transport, error) {
	c := config{
		timeout:    DefaultTimeout,
		maxRetries: DefaultMaxRetries,
		retryDelay: DefaultRetryDelay,
	}
	for _, opt := range opts {
		opt(&c)
	}
	key, err := resolveKey(c.apiKey, c.keyFunc, keyEnv...)
	if err != nil {
		return nil, err
	}
	h := c.httpClient
	if h == nil {
		// No Client.Timeout: it bounds the whole exchange, body included, and
		// would cut off a stream that is healthily reporting a long run.
		h = &http.Client{}
	}
	logger := c.logger
	if logger == nil {
		logger = newDiscardLogger()
	}
	return &transport{
		key:        key,
		baseURL:    resolveBaseURL(c.baseURL),
		http:       h,
		timeout:    c.timeout,
		maxRetries: c.maxRetries,
		retryDelay: c.retryDelay,
		user:       c.user,
		logger:     logger,
		sleep:      sleepCtx,
	}, nil
}

// who is the end-user identifier for a call.
func (t *transport) who(user string) (string, error) {
	if user != "" {
		return user, nil
	}
	if t.user != "" {
		return t.user, nil
	}
	return "", argError("Dify needs an end-user identifier for this call. Pass User in the params, or set it once with dify.WithUser(...)")
}

// send makes the request, retrying where that is safe, and turns an error
// status into an *APIError. The returned response is always a success; for a
// non-stream request its body has been read into the returned bytes.
//
// A failure before the request was written is always retried: the server
// never saw it. A failure after it was written is retried only for an
// idempotent method — repeating a timed-out POST /workflows/run can bill the
// same run again. A 429 is different: Dify refused it, so there is nothing it
// might already have done, and it is waited out for any method.
func (t *transport) send(ctx context.Context, r *request) (*http.Response, []byte, error) {
	payload, contentType, err := r.encode()
	if err != nil {
		return nil, nil, err
	}
	var key string
	if !r.noAuth {
		if key, err = t.key.reveal(ctx); err != nil {
			return nil, nil, err
		}
	}
	target := t.baseURL + r.path
	if len(r.query) > 0 {
		target += "?" + r.query.Encode()
	}

	for attempt := 0; ; attempt++ {
		// Each attempt gets its own deadline, unless the caller set one.
		actx, cancel := ctx, context.CancelFunc(func() {})
		var idle *time.Timer
		switch _, has := ctx.Deadline(); {
		case r.stream:
			// A stream is bounded by silence, not by length: the timer is
			// pushed back every time bytes arrive.
			actx, cancel = context.WithCancel(ctx)
			if t.timeout > 0 {
				idle = time.AfterFunc(t.timeout, cancel)
			}
		case !has && t.timeout > 0:
			actx, cancel = context.WithTimeout(ctx, t.timeout)
		}
		sent := false
		trace := &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) { sent = true }}
		actx = httptrace.WithClientTrace(actx, trace)

		var body io.Reader
		if payload != nil {
			body = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(actx, r.method, target, body)
		if err != nil {
			cancel()
			return nil, nil, fmt.Errorf("dify: building request: %w", err)
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if !r.noAuth {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		req.Header.Set("User-Agent", UserAgent())
		if r.stream {
			req.Header.Set("Accept", "text/event-stream")
		}

		t.logger.DebugContext(ctx, "dify request", "method", r.method, "path", r.path, "attempt", attempt+1)
		resp, err := t.http.Do(req)
		if err != nil {
			cancel()
			if ctx.Err() == context.Canceled {
				// The caller gave up; that is not a failure to retry.
				return nil, nil, ctx.Err()
			}
			timedOut := isTimeout(err)
			safe := !sent || isIdempotent(r.method)
			if safe && attempt < t.maxRetries && ctx.Err() == nil {
				delay := backoff(attempt, t.retryDelay)
				t.logger.WarnContext(ctx, "dify request failed; retrying", "method", r.method, "path", r.path, "error", err, "delay", delay)
				if serr := t.sleep(ctx, delay); serr != nil {
					return nil, nil, serr
				}
				continue
			}
			retried := 0
			if safe {
				retried = attempt
			}
			return nil, nil, &TransportError{Method: r.method, Path: r.path, Sent: sent, Retried: retried, Err: err, timeout: timedOut}
		}
		t.logger.DebugContext(ctx, "dify response", "method", r.method, "path", r.path, "status", resp.StatusCode)

		if resp.StatusCode == http.StatusTooManyRequests && attempt < t.maxRetries {
			wait, ok := rateLimitWait(resp.Header.Get("Retry-After"), attempt, t.retryDelay)
			if ok {
				drain(resp)
				cancel()
				t.logger.WarnContext(ctx, "dify rate limited; waiting", "method", r.method, "path", r.path, "wait", wait)
				if serr := t.sleep(ctx, wait); serr != nil {
					return nil, nil, serr
				}
				continue
			}
		}

		if resp.StatusCode >= 400 {
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			cancel()
			return nil, nil, apiErrorFrom(resp, raw, r.form != nil && r.form.file != nil)
		}

		if r.stream {
			// The caller owns the body now. Cancelling on close releases the
			// attempt's context along with the connection.
			resp.Body = &streamBody{ReadCloser: resp.Body, cancel: cancel, idle: idle, timeout: t.timeout}
			return resp, nil, nil
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		if err != nil {
			// The status line arrived, so the request was acted on. Not retried.
			return nil, nil, &TransportError{Method: r.method, Path: r.path, Sent: true, Err: err, timeout: isTimeout(err)}
		}
		return resp, raw, nil
	}
}

// stream sends a request whose answer is an event stream, and hands back the
// body unread.
func (t *transport) stream(ctx context.Context, r *request) (io.ReadCloser, error) {
	r.stream = true
	resp, _, err := t.send(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (t *transport) endpoint(path string) string { return t.baseURL + path }

func (t *transport) maskedKey() string { return t.key.String() }

// call sends a request and decodes a JSON answer into a map. Numbers are kept
// as json.Number so an id-like integer does not lose digits as a float.
func (t *transport) call(ctx context.Context, r *request) (object, error) {
	_, raw, err := t.send(ctx, r)
	if err != nil {
		return nil, err
	}
	return decodeObject(raw)
}

// bytes sends a request and returns the body as-is, for audio and downloads.
func (t *transport) bytes(ctx context.Context, r *request) ([]byte, http.Header, error) {
	resp, raw, err := t.send(ctx, r)
	if err != nil {
		return nil, nil, err
	}
	return raw, resp.Header, nil
}

func decodeObject(raw []byte) (object, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return object{}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("dify: decoding response: %w", err)
	}
	switch v := v.(type) {
	case map[string]any:
		return object(v), nil
	case []any:
		// A few listings answer with a bare array. Wrapped so that callers
		// have one shape to read.
		return object{"data": v}, nil
	}
	return object{"value": v}, nil
}

func apiErrorFrom(resp *http.Response, raw []byte, upload bool) *APIError {
	e := &APIError{
		StatusCode:    resp.StatusCode,
		ServerVersion: resp.Header.Get("X-Version"),
		ServerEnv:     resp.Header.Get("X-Env"),
		TraceID:       resp.Header.Get("X-Trace-Id"),
	}
	if body, err := decodeObject(raw); err == nil {
		e.Body = body
		e.Message = body.str("message")
		e.Code = body.str("code")
	} else if text := strings.TrimSpace(string(raw)); text != "" && len(text) < 300 {
		e.Message = text
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		if d, ok := retryAfter(resp.Header.Get("Retry-After")); ok {
			e.RetryAfter = d
		}
	}
	switch resp.StatusCode {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType:
		e.isUpload = upload
	}
	return e
}

// isIdempotent reports whether repeating a request has the same effect as
// making it once, so a retry after it was sent cannot duplicate anything.
func isIdempotent(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPut, http.MethodDelete:
		return true
	}
	return false
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne interface{ Timeout() bool }
	return errors.As(err, &ne) && ne.Timeout()
}

func backoff(attempt int, base time.Duration) time.Duration {
	return base * time.Duration(1<<attempt)
}

// rateLimitWait says how long to sit out a 429, or false when it should not be
// repeated at all because the server asked for longer than anyone should block.
func rateLimitWait(header string, attempt int, base time.Duration) (time.Duration, bool) {
	wait, ok := retryAfter(header)
	if !ok {
		wait = backoff(attempt, base)
	}
	if wait > MaxRateLimitWait {
		return 0, false
	}
	return wait, true
}

// retryAfter reads Retry-After, which HTTP allows to be a delay or a date.
func retryAfter(header string) (time.Duration, bool) {
	if header == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(header)); err == nil {
		return max(0, time.Duration(secs)*time.Second), true
	}
	if when, err := http.ParseTime(header); err == nil {
		return max(0, time.Until(when)), true
	}
	return 0, false
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
}

// streamBody releases a stream's context when it is closed, and pushes its
// idle deadline back whenever bytes arrive.
type streamBody struct {
	io.ReadCloser
	cancel  context.CancelFunc
	idle    *time.Timer
	timeout time.Duration
}

func (b *streamBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 && b.idle != nil {
		b.idle.Reset(b.timeout)
	}
	return n, err
}

func (b *streamBody) Close() error {
	if b.idle != nil {
		b.idle.Stop()
	}
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (d discardHandler) WithAttrs([]slog.Attr) slog.Handler      { return d }
func (d discardHandler) WithGroup(string) slog.Handler           { return d }

func newDiscardLogger() *slog.Logger { return slog.New(discardHandler{}) }

// encode renders the body once, so every attempt sends the same bytes.
func (r *request) encode() (payload []byte, contentType string, err error) {
	switch {
	case r.form != nil:
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		for k, v := range r.form.fields {
			if err := w.WriteField(k, v); err != nil {
				return nil, "", err
			}
		}
		if f := r.form.file; f != nil {
			h := make(textproto.MIMEHeader)
			h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, f.field, f.name))
			if f.contentType != "" {
				h.Set("Content-Type", f.contentType)
			} else {
				h.Set("Content-Type", "application/octet-stream")
			}
			part, err := w.CreatePart(h)
			if err != nil {
				return nil, "", err
			}
			if _, err := part.Write(f.content); err != nil {
				return nil, "", err
			}
		}
		if err := w.Close(); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), w.FormDataContentType(), nil
	case r.body != nil:
		b, err := json.Marshal(r.body)
		if err != nil {
			return nil, "", fmt.Errorf("dify: encoding request body: %w", err)
		}
		return b, "application/json", nil
	}
	return nil, "", nil
}

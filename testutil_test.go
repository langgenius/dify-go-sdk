package dify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// recorded is one request a test server saw.
type recorded struct {
	Method string
	Path   string
	Query  map[string][]string
	Header http.Header
	Body   map[string]any
	Raw    []byte
}

// fakeDify is a test server that records what it was sent and answers with
// whatever the test's handler writes.
type fakeDify struct {
	*httptest.Server
	mu       sync.Mutex
	requests []recorded
}

func newFakeDify(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *fakeDify {
	t.Helper()
	f := &fakeDify{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		rec := recorded{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Header: r.Header.Clone(), Raw: raw}
		_ = json.Unmarshal(raw, &rec.Body)
		f.mu.Lock()
		f.requests = append(f.requests, rec)
		f.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeDify) seen() []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recorded(nil), f.requests...)
}

func (f *fakeDify) last(t *testing.T) recorded {
	t.Helper()
	all := f.seen()
	if len(all) == 0 {
		t.Fatal("the server received no request")
	}
	return all[len(all)-1]
}

// app builds a client pointed at the fake, with retries that do not wait.
func (f *fakeDify) app(t *testing.T, opts ...Option) *App {
	t.Helper()
	base := []Option{WithAPIKey("app-test-key-123456"), WithBaseURL(f.URL + "/v1"), WithUser("alice")}
	a, err := NewApp(append(base, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	wire(a.api).sleep = func(ctx context.Context, d time.Duration) error { return ctx.Err() }
	return a
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeSSE(w http.ResponseWriter, events ...map[string]any) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, e := range events {
		b, _ := json.Marshal(e)
		_, _ = w.Write([]byte("data: " + string(b) + "\n\n"))
	}
}

// wire is the transport behind a client's port, for tests that change how it
// waits or read what it holds.
func wire(api port) *transport { return api.(*transport) }

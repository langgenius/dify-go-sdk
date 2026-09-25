package dify

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAnErrorCarriesTheServerThatAnswered(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Version", "1.17.1")
		w.Header().Set("X-Env", "PRODUCTION")
		writeJSON(w, 401, map[string]any{"code": "unauthorized", "message": "Access token is invalid", "status": 401})
	})
	_, err := f.app(t).Info(context.Background())

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %T %v", err, err)
	}
	if !errors.Is(err, ErrAuthentication) {
		t.Error("a 401 should match ErrAuthentication")
	}
	if apiErr.Message != "Access token is invalid" || apiErr.Code != "unauthorized" || apiErr.ServerVersion != "1.17.1" || apiErr.ServerEnv != "PRODUCTION" {
		t.Errorf("fields not carried: %+v", apiErr)
	}
	if !strings.Contains(err.Error(), "[Dify 1.17.1]") {
		t.Errorf("the version belongs in the message, got %q", err.Error())
	}
}

func TestStatusesMatchTheirSentinels(t *testing.T) {
	cases := map[int]error{401: ErrAuthentication, 404: ErrNotFound, 422: ErrValidation, 429: ErrRateLimited}
	for status, want := range cases {
		err := &APIError{StatusCode: status}
		if !errors.Is(err, want) {
			t.Errorf("%d should match %v", status, want)
		}
	}
	if errors.Is(&APIError{StatusCode: 400}, ErrValidation) {
		t.Error("a plain 400 is not a validation error")
	}
}

// failingTransport fails every request before it is written.
type failingTransport struct{ calls atomic.Int32 }

func (f *failingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.calls.Add(1)
	return nil, &net.OpError{Op: "dial", Err: errors.New("connection refused")}
}

type timeoutErr struct{}

func (timeoutErr) Error() string { return "i/o timeout" }
func (timeoutErr) Timeout() bool { return true }

func TestAFailureBeforeSendingIsRetriedForAnyMethod(t *testing.T) {
	ft := &failingTransport{}
	a, _ := NewApp(WithAPIKey("app-x"), WithBaseURL("http://dify.invalid/v1"), WithHTTPClient(&http.Client{Transport: ft}), WithUser("u"))
	a.t.sleep = func(context.Context, time.Duration) error { return nil }

	_, err := a.Workflows.Runs.Create(context.Background(), nil, nil)
	if got := ft.calls.Load(); got != 1+DefaultMaxRetries {
		t.Errorf("a connection that never opened is safe to retry: want %d attempts, got %d", 1+DefaultMaxRetries, got)
	}
	var te *TransportError
	if !errors.As(err, &te) || te.Sent || !errors.Is(err, ErrNetwork) {
		t.Errorf("want an unsent network TransportError, got %v", err)
	}
}

// sentThenTimeout writes the request, then times out waiting for the answer.
type sentThenTimeout struct{ calls atomic.Int32 }

func (s *sentThenTimeout) RoundTrip(r *http.Request) (*http.Response, error) {
	s.calls.Add(1)
	// Model what http.Transport does: report the request as written, then
	// fail reading the response.
	if trace := httptrace.ContextClientTrace(r.Context()); trace != nil && trace.WroteRequest != nil {
		trace.WroteRequest(httptrace.WroteRequestInfo{})
	}
	return nil, &net.OpError{Op: "read", Err: timeoutErr{}}
}

func TestAPostIsNotRetriedOnceItHasBeenSent(t *testing.T) {
	// A POST /workflows/run that timed out may already have run — and billed.
	// This client used to retry it three more times.
	st := &sentThenTimeout{}
	a, _ := NewApp(WithAPIKey("app-x"), WithBaseURL("http://dify.invalid/v1"), WithHTTPClient(&http.Client{Transport: st}), WithUser("u"))
	a.t.sleep = func(context.Context, time.Duration) error { return nil }

	_, err := a.Workflows.Runs.Create(context.Background(), nil, nil)
	if got := st.calls.Load(); got != 1 {
		t.Errorf("a sent POST must not be repeated, got %d attempts", got)
	}
	var te *TransportError
	if !errors.As(err, &te) || !te.Sent || !errors.Is(err, ErrTimeout) {
		t.Fatalf("want a sent timeout TransportError, got %v", err)
	}
	if !strings.Contains(err.Error(), "may already have been acted on") {
		t.Errorf("the error should say why it was not retried: %q", err)
	}
}

func TestASentGetIsRetriedBecauseItIsIdempotent(t *testing.T) {
	st := &sentThenTimeout{}
	a, _ := NewApp(WithAPIKey("app-x"), WithBaseURL("http://dify.invalid/v1"), WithHTTPClient(&http.Client{Transport: st}))
	a.t.sleep = func(context.Context, time.Duration) error { return nil }
	_, _ = a.Info(context.Background())
	if got := st.calls.Load(); got != 1+DefaultMaxRetries {
		t.Errorf("want %d attempts for a GET, got %d", 1+DefaultMaxRetries, got)
	}
}

func TestARateLimitIsWaitedOutEvenForAPost(t *testing.T) {
	var calls atomic.Int32
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "2")
			writeJSON(w, 429, map[string]any{"message": "slow down"})
			return
		}
		writeJSON(w, 200, map[string]any{"task_id": "t", "data": map[string]any{"id": "r", "status": "succeeded"}})
	})
	a := f.app(t)
	var waited []time.Duration
	a.t.sleep = func(_ context.Context, d time.Duration) error { waited = append(waited, d); return nil }

	run, err := a.Workflows.Runs.Create(context.Background(), map[string]any{"x": 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !run.Succeeded() || len(waited) != 1 || waited[0] != 2*time.Second {
		t.Errorf("a 429 was refused, so repeating it is safe; want one 2s wait, got %v", waited)
	}
}

func TestARateLimitLongerThanAMinuteIsReturnedNotWaited(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3600")
		writeJSON(w, 429, map[string]any{"message": "maintenance"})
	})
	a := f.app(t)
	a.t.sleep = func(context.Context, time.Duration) error { t.Error("should not wait an hour"); return nil }

	_, err := a.Info(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !errors.Is(err, ErrRateLimited) || apiErr.RetryAfter != time.Hour {
		t.Errorf("want a 429 carrying RetryAfter=1h, got %v", err)
	}
}

func TestRetryAfterCanBeADate(t *testing.T) {
	when := time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)
	d, ok := retryAfter(when)
	if !ok || d < 25*time.Second || d > 31*time.Second {
		t.Errorf("an HTTP date is a valid Retry-After, got %v %v", d, ok)
	}
}

func TestAnEmptyQueryValueIsLeftOffTheWire(t *testing.T) {
	// Dify's typed query models reject ?limit= where they want an int.
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"data": []any{}, "has_more": false, "limit": 20})
	})
	if _, err := f.app(t).Chat.Conversations.List(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	q := f.last(t).Query
	for _, key := range []string{"limit", "last_id", "sort_by"} {
		if _, present := q[key]; present {
			t.Errorf("%s was sent although nothing set it: %v", key, q)
		}
	}
	if q["user"][0] != "alice" {
		t.Errorf("the default user should be sent, got %v", q)
	}
}

func TestTheClientSaysWhoItIs(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{}) })
	_, _ = f.app(t).Info(context.Background())
	h := f.last(t).Header
	if !strings.HasPrefix(h.Get("User-Agent"), "dify-go-sdk/") {
		t.Errorf("User-Agent should name the SDK, got %q", h.Get("User-Agent"))
	}
	if h.Get("Authorization") != "Bearer app-test-key-123456" {
		t.Errorf("the real key signs the request, got %q", h.Get("Authorization"))
	}
}

func TestTheServerIndexIsAskedWithoutACredential(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"welcome": "Dify OpenAPI", "api_version": "v1", "server_version": "1.17.1"})
	})
	info, err := Probe(context.Background(), f.URL+"/v1")
	if err != nil {
		t.Fatal(err)
	}
	if info.ServerVersion != "1.17.1" || f.last(t).Path != "/v1/" || f.last(t).Header.Get("Authorization") != "" {
		t.Errorf("got %+v, request %+v", info, f.last(t))
	}
}

func TestTheKeyIsNeverPrinted(t *testing.T) {
	a, _ := NewApp(WithAPIKey("app-SECRETKEY123456"), WithBaseURL("http://x/v1"))
	for _, rendered := range []string{fmt.Sprint(a), fmt.Sprintf("%v", a), fmt.Sprintf("%+v", a), fmt.Sprintf("%#v", a), a.String()} {
		if strings.Contains(rendered, "SECRETKEY") {
			t.Errorf("the key leaked: %s", rendered)
		}
	}
	if got := MaskSecret("app-SECRETKEY123456"); got != "app-****3456" {
		t.Errorf("got %q", got)
	}
	if got := MaskSecret("app-short"); got != "app-****" {
		t.Errorf("a short key should not show its tail, got %q", got)
	}
}

func TestAnExplicitEmptyKeyIsAnErrorNotAFallback(t *testing.T) {
	t.Setenv(EnvAPIKey, "app-from-env")
	if _, err := NewApp(WithAPIKey("")); !errors.Is(err, ErrValidation) {
		t.Errorf("a blank in code is a mistake; got %v", err)
	}
	a, err := NewApp()
	if err != nil || a.t.key.static != "app-from-env" {
		t.Errorf("left out, the key comes from the environment: %v", err)
	}
}

func TestTheBaseURLIsDerivedFromTheHost(t *testing.T) {
	t.Setenv(EnvAPIBaseURL, "")
	t.Setenv(EnvHost, "http://localhost:8088/")
	if got := resolveBaseURL(""); got != "http://localhost:8088/v1" {
		t.Errorf("got %q", got)
	}
	t.Setenv(EnvAPIBaseURL, "https://proxy/dify/v1/")
	if got := resolveBaseURL(""); got != "https://proxy/dify/v1" {
		t.Errorf("DIFY_API_BASE_URL wins over the host, got %q", got)
	}
}

func TestAKeyFuncIsAskedPerRequest(t *testing.T) {
	var asked atomic.Int32
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{}) })
	a := f.app(t, WithAPIKeyFunc(func(context.Context) (string, error) {
		return fmt.Sprintf("app-rotated-%d", asked.Add(1)), nil
	}))
	_, _ = a.Info(context.Background())
	_, _ = a.Info(context.Background())
	if got := f.last(t).Header.Get("Authorization"); got != "Bearer app-rotated-2" {
		t.Errorf("a rotating key should be fetched each time, got %q", got)
	}
	if strings.Contains(a.String(), "rotated") {
		t.Error("printing the client should not call the key function")
	}
}

func TestAUserIsRequiredBeforeAnythingIsSent(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) { t.Error("nothing should be sent") })
	a, _ := NewApp(WithAPIKey("app-x"), WithBaseURL(f.URL+"/v1"))
	_, err := a.Chat.Messages.Create(context.Background(), "hi", nil)
	if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "WithUser") {
		t.Errorf("the error should name the fix, got %v", err)
	}
}

func TestACallerDeadlineReplacesTheDefaultTimeout(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		writeJSON(w, 200, map[string]any{"mode": "workflow"})
	})
	a := f.app(t, WithTimeout(50*time.Millisecond), WithMaxRetries(0))
	if _, err := a.Info(context.Background()); !errors.Is(err, ErrTimeout) {
		t.Errorf("the default timeout should apply, got %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := a.Info(ctx); err != nil {
		t.Errorf("a longer deadline on the context should win, got %v", err)
	}
}

func TestAnUploadRejectionMatchesErrFileUpload(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 400, map[string]any{"code": "file_too_large", "message": "File size exceeded"})
	})
	_, err := f.app(t).Files.Upload(context.Background(), FileFromReader("a.txt", strings.NewReader("hi")), "")
	if !errors.Is(err, ErrFileUpload) {
		t.Errorf("got %v", err)
	}
	if ct := f.last(t).Header.Get("Content-Type"); !strings.HasPrefix(ct, "multipart/form-data") {
		t.Errorf("an upload is multipart, got %q", ct)
	}
}

func TestAnUntypedUploadIsRefusedBeforeSending(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) { t.Error("nothing should be sent") })
	_, err := f.app(t).Files.Upload(context.Background(), FileFromReader("README", strings.NewReader("hi")), "")
	if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "415") {
		t.Errorf("Dify answers 415; the error should say so, got %v", err)
	}
}

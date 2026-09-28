package dify

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Sentinels to test an error against with errors.Is. They classify what went
// wrong without committing a caller to a concrete type:
//
//	errors.Is(err, dify.ErrAuthentication)   401 — the key is wrong or revoked
//	errors.Is(err, dify.ErrRateLimited)      429 — see APIError.RetryAfter
//	errors.Is(err, dify.ErrValidation)       422, or arguments refused before sending
//	errors.Is(err, dify.ErrFileUpload)       an upload Dify would not take
//	errors.Is(err, dify.ErrNotFound)         404
//	errors.Is(err, dify.ErrTimeout)          the request ran past its deadline
//	errors.Is(err, dify.ErrNetwork)          the connection failed
//
// errors.As(err, &apiErr) reaches the *APIError itself, which carries the
// server's message, code and version.
var (
	ErrAuthentication = errors.New("dify: authentication failed")
	ErrRateLimited    = errors.New("dify: rate limited")
	ErrValidation     = errors.New("dify: validation failed")
	ErrFileUpload     = errors.New("dify: file upload rejected")
	ErrNotFound       = errors.New("dify: not found")
	ErrTimeout        = errors.New("dify: request timed out")
	ErrNetwork        = errors.New("dify: network error")
)

// APIError is Dify answering, and the answer being an error.
//
// It carries which Dify answered. Every response Dify sends — errors included
// — has X-Version and X-Env on it, and the first question about a failing call
// is always which server and which version. By the time someone asks, the
// error message is usually all that is left, so the version goes in it.
type APIError struct {
	// StatusCode is the HTTP status. Zero for an error Dify reported inside a
	// stream after the 200 had already gone out.
	StatusCode int
	// Code is Dify's machine-readable error code, e.g. "app_unavailable".
	Code string
	// Message is what went wrong, without the server it went wrong on.
	Message string
	// Body is the decoded error body, when it was JSON.
	Body map[string]any
	// ServerVersion is the version of the Dify that answered, from X-Version.
	ServerVersion string
	// ServerEnv is PRODUCTION or TESTING, from X-Env.
	ServerEnv string
	// TraceID is Dify's own id for this request, when it is tracing.
	TraceID string
	// RetryAfter is the server's hint on a 429. Zero when it gave none.
	RetryAfter time.Duration
	// InStream is true when Dify reported this as an event mid-stream. An HTTP
	// 200 is not a successful run: a failure after the status line arrives as
	// an "error" event.
	InStream bool

	isUpload bool
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = fmt.Sprintf("HTTP %d", e.StatusCode)
	}
	if e.Code != "" {
		msg = fmt.Sprintf("%s (%s)", msg, e.Code)
	}
	if e.ServerVersion != "" {
		return fmt.Sprintf("dify: %s [Dify %s]", msg, e.ServerVersion)
	}
	return "dify: " + msg
}

// Is lets errors.Is classify an APIError by status without a type switch.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrAuthentication:
		return e.StatusCode == http.StatusUnauthorized
	case ErrRateLimited:
		return e.StatusCode == http.StatusTooManyRequests
	case ErrValidation:
		return e.StatusCode == http.StatusUnprocessableEntity
	case ErrNotFound:
		return e.StatusCode == http.StatusNotFound
	case ErrFileUpload:
		// Set when a 400 answered a request that carried a file. Kept off the
		// exported fields: it is a classification, not something Dify said.
		return e.isUpload
	}
	return false
}

// TransportError is a request that never got an answer — the connection
// failed, or it ran past its deadline.
//
// Sent reports whether the request had already gone out. A request that was
// sent and then timed out may have been acted on: a POST /workflows/run that
// timed out may still have billed a run, which is why it was not retried.
type TransportError struct {
	Method string
	Path   string
	// Sent is true when the request was written before the failure.
	Sent bool
	// Retried is how many retries were made before giving up.
	Retried int
	Err     error

	timeout bool
}

func (e *TransportError) Error() string {
	what := "network error"
	if e.timeout {
		what = "request timed out"
	}
	detail := ""
	switch {
	case e.Sent && e.Retried == 0 && !isIdempotent(e.Method):
		detail = " and was not retried: it may already have been acted on"
	case e.Retried > 0:
		detail = fmt.Sprintf(" after %d retries", e.Retried)
	}
	return fmt.Sprintf("dify: %s%s (%s %s): %v", what, detail, e.Method, e.Path, e.Err)
}

func (e *TransportError) Unwrap() error { return e.Err }

func (e *TransportError) Is(target error) bool {
	switch target {
	case ErrTimeout:
		return e.timeout
	case ErrNetwork:
		return !e.timeout
	}
	return false
}

// ArgumentError is a call this client refused before spending a request on
// it. It matches ErrValidation, the same as a 422 from Dify, because to the
// caller both mean "these arguments will not do".
type ArgumentError struct {
	Message string
}

func (e *ArgumentError) Error() string        { return "dify: " + e.Message }
func (e *ArgumentError) Is(target error) bool { return target == ErrValidation }

func argError(format string, args ...any) error {
	return &ArgumentError{Message: fmt.Sprintf(format, args...)}
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

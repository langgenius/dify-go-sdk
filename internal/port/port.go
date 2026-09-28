package port

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/langgenius/dify-go-sdk/internal/kernel"
)

// Port is everything the resources need from the wire, and all they may
// know of it. They say which operation to perform; how it is sent, retried
// and mapped to an error is the transport's business, so a resource file
// never sees an http.Client, a response, or a retry.
type Port interface {
	// Who is the end-user identifier for a call: the one given, else the
	// client's default, else an argument error naming both ways to set one.
	Who(user string) (string, error)
	// Call sends and decodes a JSON answer.
	Call(ctx context.Context, r *Request) (kernel.Object, error)
	// Bytes sends and returns the body undecoded, for audio and downloads.
	Bytes(ctx context.Context, r *Request) ([]byte, http.Header, error)
	// Stream sends and hands back the open SSE body. Closing it releases the
	// connection.
	Stream(ctx context.Context, r *Request) (io.ReadCloser, error)
	// Endpoint is the absolute URL of a path under the Service API root.
	Endpoint(path string) string
	// MaskedKey is the credential as it may be printed.
	MaskedKey() string
}

// Session is a console login, for the two things a caller does with the
// session itself rather than through it: hand its tokens to another process,
// and end it.
type Session interface {
	// Tokens are the access and CSRF tokens as they stand — renewed ones,
	// after a renewal.
	Tokens() (access, csrf string)
	// Forget drops the session, so a later request is refused here rather
	// than sent with a token that was logged out.
	Forget()
}

// Request is one call, described so that it can be sent more than once.
type Request struct {
	Method string
	Path   string
	Query  url.Values
	// Body is JSON-encoded. nil sends no body.
	Body any
	// Form, when set, is sent as multipart instead of body.
	Form *MultipartForm
	// Stream hands back the response unread, for SSE.
	Stream bool
	// NoAuth leaves the Authorization header off — the Service API index
	// takes no credential.
	NoAuth bool
}

type MultipartForm struct {
	Fields map[string]string
	File   *FilePart
}

type FilePart struct {
	Field       string
	Name        string
	ContentType string
	Content     []byte
}

// Params builds a query string, leaving out what was not set. An empty value
// on the wire is not "unset" to Dify: its typed query models reject ?limit=
// where they want an int.
type Params url.Values

func (p Params) Set(key, value string) Params {
	if value != "" {
		url.Values(p).Set(key, value)
	}
	return p
}

func (p Params) SetInt(key string, value int) Params {
	if value != 0 {
		url.Values(p).Set(key, strconv.Itoa(value))
	}
	return p
}

func (p Params) SetBool(key string, value bool) Params {
	url.Values(p).Set(key, strconv.FormatBool(value))
	return p
}

// Add appends each value under key, for a parameter Dify reads as a list —
// ?tag_ids=a&tag_ids=b, where a comma-joined value is one invalid id.
func (p Params) Add(key string, values ...string) Params {
	for _, v := range values {
		if v != "" {
			url.Values(p).Add(key, v)
		}
	}
	return p
}

func (p Params) Values() url.Values { return url.Values(p) }

// PathEscape makes an id safe to put in a path segment. Ids come back from
// Dify, but a caller can pass anything, and one with a slash in it would
// otherwise address a different route.
func PathEscape(s string) string { return url.PathEscape(s) }

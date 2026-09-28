package dify

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// port is everything the resources need from the wire, and all they may
// know of it. They say which operation to perform; how it is sent, retried
// and mapped to an error is the transport's business, so a resource file
// never sees an http.Client, a response, or a retry.
type port interface {
	// who is the end-user identifier for a call: the one given, else the
	// client's default, else an argument error naming both ways to set one.
	who(user string) (string, error)
	// call sends and decodes a JSON answer.
	call(ctx context.Context, r *request) (object, error)
	// bytes sends and returns the body undecoded, for audio and downloads.
	bytes(ctx context.Context, r *request) ([]byte, http.Header, error)
	// stream sends and hands back the open SSE body. Closing it releases the
	// connection.
	stream(ctx context.Context, r *request) (io.ReadCloser, error)
	// endpoint is the absolute URL of a path under the Service API root.
	endpoint(path string) string
	// maskedKey is the credential as it may be printed.
	maskedKey() string
}

// request is one call, described so that it can be sent more than once.
type request struct {
	method string
	path   string
	query  url.Values
	// body is JSON-encoded. nil sends no body.
	body any
	// form, when set, is sent as multipart instead of body.
	form *multipartForm
	// stream hands back the response unread, for SSE.
	stream bool
	// noAuth leaves the Authorization header off — the Service API index
	// takes no credential.
	noAuth bool
}

type multipartForm struct {
	fields map[string]string
	file   *filePart
}

type filePart struct {
	field       string
	name        string
	contentType string
	content     []byte
}

// params builds a query string, leaving out what was not set. An empty value
// on the wire is not "unset" to Dify: its typed query models reject ?limit=
// where they want an int.
type params url.Values

func (p params) set(key, value string) params {
	if value != "" {
		url.Values(p).Set(key, value)
	}
	return p
}

func (p params) setInt(key string, value int) params {
	if value != 0 {
		url.Values(p).Set(key, strconv.Itoa(value))
	}
	return p
}

func (p params) setBool(key string, value bool) params {
	url.Values(p).Set(key, strconv.FormatBool(value))
	return p
}

func (p params) values() url.Values { return url.Values(p) }

// pathEscape makes an id safe to put in a path segment. Ids come back from
// Dify, but a caller can pass anything, and one with a slash in it would
// otherwise address a different route.
func pathEscape(s string) string { return url.PathEscape(s) }

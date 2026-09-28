package usecase

import (
	"context"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"github.com/langgenius/dify-go-sdk/internal/codec"
	"github.com/langgenius/dify-go-sdk/internal/entity"
	"github.com/langgenius/dify-go-sdk/internal/kernel"
	"github.com/langgenius/dify-go-sdk/internal/port"
)

// Upload is something to upload: a name, its bytes, and its type.
type Upload struct {
	// Name is the recorded file name. Dify types an upload by its extension
	// and rejects one it cannot type.
	Name string
	// ContentType overrides the type guessed from Name.
	ContentType string
	Reader      io.Reader

	// path is read when the upload is sent, so FileFromPath needs no error
	// return and leaves no descriptor open if the upload is refused first.
	path string
}

// FileFromPath uploads the file at path, under its base name.
func FileFromPath(path string) Upload {
	return Upload{Name: filepath.Base(path), path: path}
}

// FileFromReader uploads what r yields, recorded as name.
func FileFromReader(name string, r io.Reader) Upload { return Upload{Name: name, Reader: r} }

func (u Upload) read() ([]byte, error) {
	if u.path != "" {
		return os.ReadFile(u.path)
	}
	if u.Reader == nil {
		return nil, kernel.ArgError("the upload %q has nothing to read", u.Name)
	}
	return io.ReadAll(u.Reader)
}

// part works out the (name, bytes, type) Dify's upload wants. An untyped
// upload is refused here, because Dify answers it with 415.
func (u Upload) part(field string, requireType bool) (*port.FilePart, error) {
	if u.Name == "" {
		return nil, kernel.ArgError("this upload has no name to record; Dify types an upload by its extension and rejects one it cannot type")
	}
	contentType := u.ContentType
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(u.Name))
	}
	if contentType == "" && requireType {
		return nil, kernel.ArgError("cannot tell what kind of file %q is; set Upload.ContentType — Dify answers an untyped upload with 415", u.Name)
	}
	content, err := u.read()
	if err != nil {
		return nil, err
	}
	return &port.FilePart{Field: field, Name: filepath.Base(u.Name), ContentType: contentType, Content: content}, nil
}

// Files are the files this app's runs and messages can reference.
type Files struct{ api port.Port }

// Upload uploads a file and returns the reference to it. user empty uses
// WithUser.
func (f *Files) Upload(ctx context.Context, file Upload, user string) (*entity.UploadedFile, error) {
	who, err := f.api.Who(user)
	if err != nil {
		return nil, err
	}
	part, err := file.part("file", true)
	if err != nil {
		return nil, err
	}
	o, err := f.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/files/upload", Form: &port.MultipartForm{Fields: map[string]string{"user": who}, File: part}})
	if err != nil {
		return nil, err
	}
	return codec.UploadedFrom(o), nil
}

// Download is the bytes of a file that a message carries.
//
// Not the counterpart to Upload, despite appearances: Dify serves this only
// for files attached to a message in this app. A file uploaded but not yet
// used in a conversation answers "The requested file was not found", which
// reads like a wrong id when it is not.
func (f *Files) Download(ctx context.Context, fileID string, asAttachment bool) ([]byte, http.Header, error) {
	return f.api.Bytes(ctx, &port.Request{
		Method: http.MethodGet,
		Path:   "/files/" + port.PathEscape(fileID) + "/preview",
		Query:  port.Params{}.SetBool("as_attachment", asAttachment).Values(),
	})
}

// PreviewURL is where Dify serves a file. Reaching it needs the app's key, so
// the URL alone is not enough for a browser — use Download.
func (f *Files) PreviewURL(fileID string) string {
	return f.api.Endpoint("/files/" + port.PathEscape(fileID) + "/preview")
}

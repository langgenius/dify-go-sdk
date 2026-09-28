package dify

import (
	"context"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
)

// UploadedFile is a file Dify has taken, and the reference that points at it.
type UploadedFile struct {
	ID        string
	Name      string
	Size      int64
	MimeType  string
	Extension string
	// CreatedBy is an end-user id; App.EndUser resolves it.
	CreatedBy string
	CreatedAt *int64
	Raw       map[string]any
}

// Reference is the mapping a run's or a message's inputs use to name this
// file. Dify takes a reference, not the bytes:
//
//	f, _ := app.Files.Upload(ctx, dify.FileFromPath("report.pdf"), nil)
//	app.Workflows.Runs.Create(ctx, map[string]any{"doc": f.Reference("document")}, nil)
//
// kind is document, image, audio, video or custom.
func (f *UploadedFile) Reference(kind string) map[string]any {
	if kind == "" {
		kind = "document"
	}
	return map[string]any{"transfer_method": "local_file", "upload_file_id": f.ID, "type": kind}
}

func uploadedFrom(o object) *UploadedFile {
	return &UploadedFile{
		ID:        o.str("id"),
		Name:      o.str("name"),
		Size:      o.int64("size"),
		MimeType:  o.str("mime_type"),
		Extension: o.str("extension"),
		CreatedBy: o.str("created_by"),
		CreatedAt: o.intPtr("created_at"),
		Raw:       o.raw(),
	}
}

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
		return nil, argError("the upload %q has nothing to read", u.Name)
	}
	return io.ReadAll(u.Reader)
}

// part works out the (name, bytes, type) Dify's upload wants. An untyped
// upload is refused here, because Dify answers it with 415.
func (u Upload) part(field string, requireType bool) (*filePart, error) {
	if u.Name == "" {
		return nil, argError("this upload has no name to record; Dify types an upload by its extension and rejects one it cannot type")
	}
	contentType := u.ContentType
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(u.Name))
	}
	if contentType == "" && requireType {
		return nil, argError("cannot tell what kind of file %q is; set Upload.ContentType — Dify answers an untyped upload with 415", u.Name)
	}
	content, err := u.read()
	if err != nil {
		return nil, err
	}
	return &filePart{field: field, name: filepath.Base(u.Name), contentType: contentType, content: content}, nil
}

// Files are the files this app's runs and messages can reference.
type Files struct{ api port }

// Upload uploads a file and returns the reference to it. user empty uses
// WithUser.
func (f *Files) Upload(ctx context.Context, file Upload, user string) (*UploadedFile, error) {
	who, err := f.api.who(user)
	if err != nil {
		return nil, err
	}
	part, err := file.part("file", true)
	if err != nil {
		return nil, err
	}
	o, err := f.api.call(ctx, &request{method: http.MethodPost, path: "/files/upload", form: &multipartForm{fields: map[string]string{"user": who}, file: part}})
	if err != nil {
		return nil, err
	}
	return uploadedFrom(o), nil
}

// Download is the bytes of a file that a message carries.
//
// Not the counterpart to Upload, despite appearances: Dify serves this only
// for files attached to a message in this app. A file uploaded but not yet
// used in a conversation answers "The requested file was not found", which
// reads like a wrong id when it is not.
func (f *Files) Download(ctx context.Context, fileID string, asAttachment bool) ([]byte, http.Header, error) {
	return f.api.bytes(ctx, &request{
		method: http.MethodGet,
		path:   "/files/" + pathEscape(fileID) + "/preview",
		query:  params{}.setBool("as_attachment", asAttachment).values(),
	})
}

// PreviewURL is where Dify serves a file. Reaching it needs the app's key, so
// the URL alone is not enough for a browser — use Download.
func (f *Files) PreviewURL(fileID string) string {
	return f.api.endpoint("/files/" + pathEscape(fileID) + "/preview")
}

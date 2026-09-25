package dify

import (
	"context"
	"net/http"
)

// MetadataField is a field documents in one knowledge base may carry.
//
// Two kinds arrive in this shape and the difference is the id: a field you
// defined has one and can be renamed or deleted by it, while one of Dify's
// own — filename, upload date — has none, because those are turned on and
// off rather than managed.
type MetadataField struct {
	Name string
	Type string
	ID   string
	// Count is how many documents have a value for it. Absent (zero) on
	// Dify's own fields.
	Count int
	Raw   map[string]any
}

// BuiltIn reports whether Dify fills this field in rather than you.
func (m *MetadataField) BuiltIn() bool { return m.ID == "" }

func (m *MetadataField) String() string { return m.Name }

func metadataFieldFrom(o object) *MetadataField {
	return &MetadataField{
		Name:  o.str("name"),
		Type:  o.str("type"),
		ID:    o.str("id"),
		Count: o.int("count"),
		Raw:   o.raw(),
	}
}

// Metadata lists the metadata fields this knowledge base defines, custom and
// built-in.
func (d *Datasets) Metadata(ctx context.Context, datasetID string) ([]*MetadataField, error) {
	o, err := d.t.call(ctx, &request{method: http.MethodGet, path: datasetPath(datasetID, "metadata")})
	if err != nil {
		return nil, err
	}
	items := o.objs("doc_metadata")
	if len(items) == 0 {
		items = o.objs("data")
	}
	return buildAll(items, metadataFieldFrom), nil
}

// AddMetadataField defines a metadata field documents in this base may
// carry. fieldType defaults to "string".
func (d *Datasets) AddMetadataField(ctx context.Context, datasetID, name, fieldType string) (*MetadataField, error) {
	o, err := d.t.call(ctx, &request{method: http.MethodPost, path: datasetPath(datasetID, "metadata"), body: map[string]any{"name": name, "type": firstNonZero(fieldType, "string")}})
	if err != nil {
		return nil, err
	}
	return metadataFieldFrom(o), nil
}

// RenameMetadataField renames a metadata field. The values on documents are
// kept.
func (d *Datasets) RenameMetadataField(ctx context.Context, datasetID, fieldID, name string) (*MetadataField, error) {
	o, err := d.t.call(ctx, &request{method: http.MethodPatch, path: datasetPath(datasetID, "metadata", pathEscape(fieldID)), body: map[string]any{"name": name}})
	if err != nil {
		return nil, err
	}
	return metadataFieldFrom(o), nil
}

// DeleteMetadataField removes a metadata field, and its values from every
// document.
func (d *Datasets) DeleteMetadataField(ctx context.Context, datasetID, fieldID string) error {
	_, err := d.t.call(ctx, &request{method: http.MethodDelete, path: datasetPath(datasetID, "metadata", pathEscape(fieldID))})
	return err
}

// BuiltInMetadata lists the fields Dify maintains itself — filename, upload
// date and such. Separate from AddMetadataField's fields: these are filled
// in for you, and are turned on or off rather than created, which is why
// they carry no id (MetadataField.BuiltIn reads that).
func (d *Datasets) BuiltInMetadata(ctx context.Context, datasetID string) ([]*MetadataField, error) {
	o, err := d.t.call(ctx, &request{method: http.MethodGet, path: datasetPath(datasetID, "metadata", "built-in")})
	if err != nil {
		return nil, err
	}
	items := o.objs("fields")
	if len(items) == 0 {
		items = o.objs("data")
	}
	return buildAll(items, metadataFieldFrom), nil
}

// SetBuiltInMetadata turns Dify's own metadata fields on or off for this
// knowledge base.
func (d *Datasets) SetBuiltInMetadata(ctx context.Context, datasetID string, enabled bool) error {
	action := "disable"
	if enabled {
		action = "enable"
	}
	_, err := d.t.call(ctx, &request{method: http.MethodPost, path: datasetPath(datasetID, "metadata", "built-in", action)})
	return err
}

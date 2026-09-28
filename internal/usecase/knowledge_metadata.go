package usecase

import (
	"context"
	"net/http"

	"github.com/langgenius/dify-go-sdk/internal/codec"
	"github.com/langgenius/dify-go-sdk/internal/entity"
	"github.com/langgenius/dify-go-sdk/internal/kernel"
	"github.com/langgenius/dify-go-sdk/internal/port"
)

// Metadata lists the metadata fields this knowledge base defines, custom and
// built-in.
func (d *Datasets) Metadata(ctx context.Context, datasetID string) ([]*entity.MetadataField, error) {
	o, err := d.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: datasetPath(datasetID, "metadata")})
	if err != nil {
		return nil, err
	}
	return codec.MetadataFieldsFrom(o, "doc_metadata"), nil
}

// AddMetadataField defines a metadata field documents in this base may
// carry. fieldType defaults to "string".
func (d *Datasets) AddMetadataField(ctx context.Context, datasetID, name, fieldType string) (*entity.MetadataField, error) {
	o, err := d.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: datasetPath(datasetID, "metadata"), Body: map[string]any{"name": name, "type": kernel.FirstNonZero(fieldType, "string")}})
	if err != nil {
		return nil, err
	}
	return codec.MetadataFieldFrom(o), nil
}

// RenameMetadataField renames a metadata field. The values on documents are
// kept.
func (d *Datasets) RenameMetadataField(ctx context.Context, datasetID, fieldID, name string) (*entity.MetadataField, error) {
	o, err := d.api.Call(ctx, &port.Request{Method: http.MethodPatch, Path: datasetPath(datasetID, "metadata", port.PathEscape(fieldID)), Body: map[string]any{"name": name}})
	if err != nil {
		return nil, err
	}
	return codec.MetadataFieldFrom(o), nil
}

// DeleteMetadataField removes a metadata field, and its values from every
// document.
func (d *Datasets) DeleteMetadataField(ctx context.Context, datasetID, fieldID string) error {
	_, err := d.api.Call(ctx, &port.Request{Method: http.MethodDelete, Path: datasetPath(datasetID, "metadata", port.PathEscape(fieldID))})
	return err
}

// BuiltInMetadata lists the fields Dify maintains itself — filename, upload
// date and such. Separate from AddMetadataField's fields: these are filled
// in for you, and are turned on or off rather than created, which is why
// they carry no id (MetadataField.BuiltIn reads that).
func (d *Datasets) BuiltInMetadata(ctx context.Context, datasetID string) ([]*entity.MetadataField, error) {
	o, err := d.api.Call(ctx, &port.Request{Method: http.MethodGet, Path: datasetPath(datasetID, "metadata", "built-in")})
	if err != nil {
		return nil, err
	}
	return codec.MetadataFieldsFrom(o, "fields"), nil
}

// SetBuiltInMetadata turns Dify's own metadata fields on or off for this
// knowledge base.
func (d *Datasets) SetBuiltInMetadata(ctx context.Context, datasetID string, enabled bool) error {
	action := "disable"
	if enabled {
		action = "enable"
	}
	_, err := d.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: datasetPath(datasetID, "metadata", "built-in", action)})
	return err
}

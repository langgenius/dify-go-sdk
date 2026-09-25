package dify

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// TestEveryTagVerbReachesItsRoute pins each workspace-level Tags verb to the
// method, path and body Dify's controller defines.
func TestEveryTagVerbReachesItsRoute(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		call   func(tg *Tags) error
		method string
		path   string
		body   map[string]any
	}{
		{"list", func(tg *Tags) error { _, err := tg.List(ctx); return err }, "GET", "/v1/datasets/tags", nil},
		{"create", func(tg *Tags) error { _, err := tg.Create(ctx, "billing"); return err }, "POST", "/v1/datasets/tags", map[string]any{"name": "billing", "type": "knowledge"}},
		{"rename", func(tg *Tags) error { _, err := tg.Rename(ctx, "t1", "renamed"); return err }, "PATCH", "/v1/datasets/tags", map[string]any{"tag_id": "t1", "name": "renamed"}},
		{"delete", func(tg *Tags) error { return tg.Delete(ctx, "t1") }, "DELETE", "/v1/datasets/tags", map[string]any{"tag_id": "t1"}},
		{"bind", func(tg *Tags) error { return tg.Bind(ctx, "d1", []string{"t1", "t2"}) }, "POST", "/v1/datasets/tags/binding", map[string]any{"target_id": "d1"}},
		{"unbind", func(tg *Tags) error { return tg.Unbind(ctx, "d1", "t1", "t2") }, "POST", "/v1/datasets/tags/unbinding", map[string]any{"target_id": "d1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, 200, map[string]any{"id": "t1", "name": "billing", "type": "knowledge", "binding_count": "3"})
			})
			if err := tc.call(knowledgeClient(t, f).Tags); err != nil {
				t.Fatal(err)
			}
			got := f.last(t)
			if got.Method != tc.method || got.Path != tc.path {
				t.Fatalf("want %s %s, got %s %s", tc.method, tc.path, got.Method, got.Path)
			}
			for k, v := range tc.body {
				if got.Body[k] != v {
					t.Errorf("body[%s] = %v, want %v (body %v)", k, got.Body[k], v, got.Body)
				}
			}
		})
	}
}

// TestUnbindSendsTagIDsPluralNeverTheDeprecatedSingularField pins that this
// SDK always sends tag_ids, the array Dify's TagUnbindingPayload documents as
// current — never the singular tag_id, which the same payload accepts but
// marks deprecated in its own JSON schema.
func TestUnbindSendsTagIDsPluralNeverTheDeprecatedSingularField(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{}) })
	if err := knowledgeClient(t, f).Tags.Unbind(context.Background(), "d1", "t1"); err != nil {
		t.Fatal(err)
	}
	body := f.last(t).Body
	if _, has := body["tag_id"]; has {
		t.Errorf("sent the deprecated singular tag_id: %v", body)
	}
	ids, _ := body["tag_ids"].([]any)
	if len(ids) != 1 || ids[0] != "t1" {
		t.Errorf("got tag_ids=%v", body["tag_ids"])
	}
}

// TestUnbindRefusesAnEmptyTagList pins that naming at least one tag is
// required before a request is sent at all.
func TestUnbindRefusesAnEmptyTagList(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("should not have sent a request") })
	err := knowledgeClient(t, f).Tags.Unbind(context.Background(), "d1")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("got %v, want ErrValidation", err)
	}
}

// TestBindingCountArrivesAsAStringAndIsParsed pins the AGENTS.md warning:
// Dify sends binding_count as a string, so a naive numeric comparison on the
// raw payload would compare a string to an int.
func TestBindingCountArrivesAsAStringAndIsParsed(t *testing.T) {
	tag := tagFrom(object{"id": "t1", "name": "billing", "binding_count": "12"})
	if tag.BindingCount != 12 {
		t.Fatalf("got %d, want 12", tag.BindingCount)
	}
}

// TestBindingCountIsZeroWhenNotAPlainDigitString pins the same rule the
// Python SDK applies: only a string of plain digits is parsed; anything
// else — absent, null, non-numeric — reads as zero rather than panicking or
// guessing.
func TestBindingCountIsZeroWhenNotAPlainDigitString(t *testing.T) {
	for _, raw := range []any{nil, "", "abc", "-1"} {
		tag := tagFrom(object{"id": "t1", "binding_count": raw})
		if tag.BindingCount != 0 {
			t.Errorf("binding_count=%v got %d, want 0", raw, tag.BindingCount)
		}
	}
}

// TestTagsListAnswersABareArrayAsAPageOfEverything pins that Dify's tag
// listing carries no paging envelope at all, and this SDK still hands back a
// Page rather than a bare slice, so a caller need not know which listings
// page and which do not.
func TestTagsListAnswersABareArrayAsAPageOfEverything(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"id":"t1","name":"billing"},{"id":"t2","name":"support"}]`))
	})
	page, err := knowledgeClient(t, f).Tags.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.HasMore {
		t.Fatalf("got %+v", page)
	}
	next, err := page.NextPage(context.Background())
	if err != nil || next != nil {
		t.Fatalf("an unpaged listing should not continue: %v %v", next, err)
	}
}

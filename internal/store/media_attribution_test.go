package store

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestMediaAttributionIsRequiredAndMetadataIsImmutable(t *testing.T) {
	s, _ := accountSite(t)
	ctx := context.Background()

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO media(id, storage_name, kind, size, width, height, created_by)
		VALUES(?, ?, ?, ?, ?, ?, NULL)`,
		"null-attribution", "unattributed.pdf", "pdf", 12, nil, nil); err == nil {
		t.Fatal("media insert with NULL created_by succeeded")
	}
	var unattributedRows int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM media WHERE id = ?", "null-attribution").Scan(&unattributedRows); err != nil {
		t.Fatal(err)
	}
	if unattributedRows != 0 {
		t.Fatalf("rejected NULL created_by insert left %d rows, want 0", unattributedRows)
	}

	width, height := int64(1280), int64(720)
	draft := MediaDraft{
		StorageName: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.png",
		Kind:        "png",
		Size:        4096,
		Width:       &width,
		Height:      &height,
		CreatedBy:   "018f47a2-4b9c-7abc-8def-0123456789c0",
	}
	id, err := s.CreateMedia(ctx, draft, time.Date(2026, 10, 4, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	want := Media{
		ID: id, StorageName: draft.StorageName, Kind: draft.Kind, Size: draft.Size,
		Width: draft.Width, Height: draft.Height, CreatedBy: draft.CreatedBy,
	}

	assertUnchanged := func() {
		t.Helper()
		got, err := s.GetMedia(ctx, id)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("media after rejected update = %#v, %v; want %#v", got, err, want)
		}
	}

	if _, err := s.db.ExecContext(ctx, "UPDATE media SET created_by = ? WHERE id = ?", "018f47a2-4b9c-7abc-8def-0123456789c1", id); err == nil {
		t.Fatal("media created_by update succeeded")
	}
	assertUnchanged()

	if _, err := s.db.ExecContext(ctx, "UPDATE media SET storage_name = ? WHERE id = ?", "changed.png", id); err == nil {
		t.Fatal("media storage_name update succeeded")
	}
	assertUnchanged()
}

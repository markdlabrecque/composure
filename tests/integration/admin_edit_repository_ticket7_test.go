package integration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/content"
	"github.com/markdlabrecque/composure/internal/site"
)

func TestPhase1EditDraftRepositoryReportsChanged(t *testing.T) {
	path := initSite(t, false)
	db := openDB(t, path)
	opened, err := site.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	var repository content.Repository = opened
	ctx := context.Background()
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	draft := content.ItemDraft{Title: "Original", Path: "/draft", Fields: map[string]string{"body": "Original body"}}
	id, err := repository.CreateItem(ctx, draft, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	before := admin6State(t, db)
	draft.Title = "Changed"
	draft.Fields["body"] = "Changed body"
	changedAt := createdAt.Add(time.Hour)
	changed, err := repository.SaveDraft(ctx, id, 1, draft, changedAt, "local-prototype")
	if err != nil || !changed {
		t.Fatalf("changed save result=(%v,%v), want (true,nil)", changed, err)
	}
	item, err := repository.GetItem(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if item.Revision != 2 || item.Title != draft.Title || item.Path != draft.Path || item.Fields["body"] != draft.Fields["body"] || item.CreatedAt != createdAt.Format("2006-01-02T15:04:05.000Z") || item.UpdatedAt != changedAt.Format("2006-01-02T15:04:05.000Z") {
		t.Errorf("changed save persisted wrong values: %+v", item)
	}
	admin7NoHistory(t, db, before)
	if _, err := db.Exec(`CREATE TRIGGER ticket7_repository_no_update BEFORE UPDATE ON items BEGIN SELECT RAISE(ABORT,'unchanged save attempted UPDATE'); END`); err != nil {
		t.Fatal(err)
	}
	noOpState := admin6State(t, db)
	changed, err = repository.SaveDraft(ctx, id, 2, draft, changedAt.Add(time.Hour), "local-prototype")
	if err != nil || changed {
		t.Errorf("unchanged save result=(%v,%v), want (false,nil)", changed, err)
	}
	admin6Unchanged(t, db, noOpState)
	changed, err = repository.SaveDraft(ctx, id, 1, draft, changedAt.Add(time.Hour), "local-prototype")
	if changed || !errors.Is(err, content.ErrStaleDraft) {
		t.Errorf("stale identical save result=(%v,%v), want (false,ErrStaleDraft)", changed, err)
	}
	admin6Unchanged(t, db, noOpState)
	// An attempted changed save rejected by SQLite cannot report committed change.
	draft.Title = "Blocked"
	changed, err = repository.SaveDraft(ctx, id, 2, draft, changedAt.Add(time.Hour), "local-prototype")
	if changed || err == nil {
		t.Errorf("failed write result=(%v,%v), want (false,error)", changed, err)
	}
	admin6Unchanged(t, db, noOpState)
}

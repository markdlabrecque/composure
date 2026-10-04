package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/content"
)

func TestMediaRoundTripRestartAndSiteIsolation(t *testing.T) {
	s, path := accountSite(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 3, 12, 34, 56, 789123456, time.FixedZone("offset", -7*60*60))
	width, height := int64(1600), int64(900)
	drafts := []MediaDraft{
		{StorageName: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef.png", Kind: "png", Size: 123456, Width: &width, Height: &height, CreatedBy: "018f47a2-4b9c-7abc-8def-0123456789c0"},
		{StorageName: "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210.pdf", Kind: "pdf", Size: 987654, Width: nil, Height: nil, CreatedBy: "018f47a2-4b9c-7abc-8def-0123456789c1"},
	}

	want := make([]Media, len(drafts))
	for i, draft := range drafts {
		id, err := s.CreateMedia(ctx, draft, at)
		if err != nil {
			t.Fatal(err)
		}
		if id == "" {
			t.Fatal("CreateMedia returned an empty ID")
		}
		want[i] = Media{
			ID: id, StorageName: draft.StorageName, Kind: draft.Kind, Size: draft.Size,
			Width: draft.Width, Height: draft.Height, CreatedBy: draft.CreatedBy,
		}
	}
	if want[0].ID == want[1].ID {
		t.Fatal("CreateMedia generated duplicate IDs")
	}

	verify := func(repository *Store) {
		t.Helper()
		for _, expected := range want {
			got, err := repository.GetMedia(ctx, expected.ID)
			if err != nil || !reflect.DeepEqual(got, expected) {
				t.Fatalf("media round trip = %#v, %v; want %#v", got, err, expected)
			}
		}
	}
	verify(s)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	verify(reopened)

	other, _ := accountSite(t)
	if got, err := other.GetMedia(ctx, want[0].ID); !errors.Is(err, content.ErrNotFound) || !reflect.DeepEqual(got, Media{}) {
		t.Fatalf("media crossed site boundary: %#v, %v", got, err)
	}
}

func TestMediaAllowsDistinctRowsForSameStorageName(t *testing.T) {
	s, _ := accountSite(t)
	draft := MediaDraft{StorageName: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.pdf", Kind: "pdf", Size: 42, CreatedBy: "018f47a2-4b9c-7abc-8def-0123456789c0"}
	at := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

	first, err := s.CreateMedia(context.Background(), draft, at)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateMedia(context.Background(), draft, at)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || second == "" || first == second {
		t.Fatalf("same storage name IDs = %q and %q, want two distinct generated IDs", first, second)
	}
	for _, id := range []string{first, second} {
		got, err := s.GetMedia(context.Background(), id)
		if err != nil || got.ID != id || got.StorageName != draft.StorageName {
			t.Fatalf("stored media %q = %#v, %v", id, got, err)
		}
	}
}

func TestMediaCreatedBySurvivesAccountDeactivation(t *testing.T) {
	s, _ := accountSite(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	accountID, err := s.CreateAccount(ctx, AccountDraft{Email: "uploader@example.test", PasswordHash: "hash", IsEditor: true, State: "active"}, at)
	if err != nil {
		t.Fatal(err)
	}
	mediaID, err := s.CreateMedia(ctx, MediaDraft{StorageName: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.pdf", Kind: "pdf", Size: 64, CreatedBy: accountID}, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeactivateAccount(ctx, accountID, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMedia(ctx, mediaID)
	if err != nil || got.CreatedBy != accountID {
		t.Fatalf("media attribution after deactivation = %q, %v; want %q", got.CreatedBy, err, accountID)
	}
}

func TestMediaMissingCanceledAndClosedErrors(t *testing.T) {
	s, _ := accountSite(t)
	ctx := context.Background()
	if got, err := s.GetMedia(ctx, "missing"); !errors.Is(err, content.ErrNotFound) || !reflect.DeepEqual(got, Media{}) {
		t.Fatalf("missing media = %#v, %v; want zero Media and ErrNotFound", got, err)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	draft := MediaDraft{StorageName: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc.pdf", Kind: "pdf", Size: 10, CreatedBy: "018f47a2-4b9c-7abc-8def-0123456789c0"}
	if id, err := s.CreateMedia(canceled, draft, time.Now()); err == nil || id != "" {
		t.Fatalf("canceled create = %q, %v; want empty ID and error", id, err)
	}
	if got, err := s.GetMedia(canceled, "missing"); err == nil || !reflect.DeepEqual(got, Media{}) {
		t.Fatalf("canceled get = %#v, %v; want zero Media and error", got, err)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM media").Scan(&count); err != nil || count != 0 {
		t.Fatalf("canceled create left %d rows: %v", count, err)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if id, err := s.CreateMedia(ctx, draft, time.Now()); err == nil || id != "" {
		t.Fatalf("closed create = %q, %v; want empty ID and error", id, err)
	}
	if got, err := s.GetMedia(ctx, "missing"); err == nil || !reflect.DeepEqual(got, Media{}) {
		t.Fatalf("closed get = %#v, %v; want zero Media and error", got, err)
	}
}

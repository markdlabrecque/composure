package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/content"
)

func TestAccountRoundTripAndRestart(t *testing.T) {
	s, path := accountSite(t)
	ctx := context.Background()
	at := time.Date(2026, 9, 30, 12, 34, 56, 789123456, time.FixedZone("offset", -7*3600))
	draft := AccountDraft{Email: "  ADMIN+Tag@Example.TEST  ", PasswordHash: "opaque encoded hash with arbitrary parameters", IsAdministrator: true, IsEditor: true, State: "active"}
	id, err := s.CreateAccount(ctx, draft, at)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(id) {
		t.Fatalf("not UUIDv7: %q", id)
	}
	ms, err := strconv.ParseInt(strings.ReplaceAll(id[:13], "-", ""), 16, 64)
	if err != nil || ms != at.UnixMilli() {
		t.Fatalf("UUID clock=%d, want %d; %v", ms, at.UnixMilli(), err)
	}
	want := Account{ID: id, Email: "admin+tag@example.test", PasswordHash: draft.PasswordHash, IsAdministrator: true, IsEditor: true, State: "active", CreatedAt: "2026-09-30T19:34:56.789Z", UpdatedAt: "2026-09-30T19:34:56.789Z"}
	verify := func(s *Store) {
		t.Helper()
		byID, err := s.GetAccount(ctx, id)
		if err != nil || !reflect.DeepEqual(byID, want) {
			t.Fatalf("id round trip differs: %v", err)
		}
		byEmail, err := s.GetAccountByEmail(ctx, " ADMIN+TAG@EXAMPLE.TEST ")
		if err != nil || !reflect.DeepEqual(byEmail, want) {
			t.Fatalf("email round trip differs: %v", err)
		}
		list, err := s.ListAccounts(ctx)
		if err != nil || !reflect.DeepEqual(list, []Account{want}) {
			t.Fatalf("list round trip differs: %v", err)
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
}

func TestAccountEmailNormalizationBoundaries(t *testing.T) {
	s, _ := accountSite(t)
	ctx := context.Background()
	at := time.Now()
	cases := []struct{ input, want string }{
		{" Mixed@Example.TEST ", "mixed@example.test"},
		{"ÄÉ@EXAMPLE.TEST", "ÄÉ@example.test"},
		{"äé@example.test", "äé@example.test"},
		{"\u00a0User@EXAMPLE.TEST\u00a0", "\u00a0user@example.test\u00a0"},
		{"\tUser@EXAMPLE.TEST\t", "\tuser@example.test\t"},
		{"A.B+Tag@EXAMPLE.TEST", "a.b+tag@example.test"},
		{"AB+Tag@EXAMPLE.TEST", "ab+tag@example.test"},
		{"A.B@EXAMPLE.TEST", "a.b@example.test"},
		{"E\u0301@EXAMPLE.TEST", "e\u0301@example.test"},
	}
	ids := map[string]bool{}
	for _, tc := range cases {
		id, err := s.CreateAccount(ctx, AccountDraft{Email: tc.input, PasswordHash: "hash", IsEditor: true, State: "active"}, at)
		if err != nil {
			t.Fatalf("create boundary address: %v", err)
		}
		if ids[id] {
			t.Fatal("generated duplicate ID")
		}
		ids[id] = true
		got, err := s.GetAccountByEmail(ctx, tc.input)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != id || got.Email != tc.want {
			t.Fatalf("normalization got email %q, want %q", got.Email, tc.want)
		}
	}
	list, err := s.ListAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != len(cases) {
		t.Fatalf("list count %d, want %d", len(list), len(cases))
	}
	for i := 1; i < len(list); i++ {
		if list[i-1].Email > list[i].Email || (list[i-1].Email == list[i].Email && list[i-1].ID > list[i].ID) {
			t.Fatal("list must sort by email then ID")
		}
	}
}

func TestAccountStatesRolesAndFailedCreates(t *testing.T) {
	s, _ := accountSite(t)
	ctx := context.Background()
	at := time.Now()
	for i, roles := range [][2]bool{{true, false}, {false, true}, {true, true}} {
		state := "active"
		if i == 1 {
			state = "deactivated"
		}
		draft := AccountDraft{Email: fmt.Sprintf("role%d@example.test", i), PasswordHash: "hash", IsAdministrator: roles[0], IsEditor: roles[1], State: state}
		id, err := s.CreateAccount(ctx, draft, at)
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.GetAccount(ctx, id)
		if err != nil || got.IsAdministrator != roles[0] || got.IsEditor != roles[1] || got.State != state {
			t.Fatalf("independent roles/state changed: %v", err)
		}
	}
	before, err := s.ListAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, draft := range []AccountDraft{
		{Email: " ROLE1@EXAMPLE.TEST ", PasswordHash: "other", IsAdministrator: true, State: "active"},
		{Email: "noroles@example.test", PasswordHash: "hash", State: "active"},
		{Email: "state@example.test", PasswordHash: "hash", IsEditor: true, State: "pending"},
	} {
		if id, err := s.CreateAccount(ctx, draft, at); err == nil || id != "" {
			t.Fatal("invalid or duplicate account create must fail without an ID")
		}
	}
	after, err := s.ListAccounts(ctx)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("failed create altered accounts: %v", err)
	}
}

func TestAccountMissingLookups(t *testing.T) {
	s, _ := accountSite(t)
	ctx := context.Background()
	if list, err := s.ListAccounts(ctx); err != nil || len(list) != 0 {
		t.Fatalf("empty listing: %v", err)
	}
	if _, err := s.GetAccount(ctx, "missing"); !errors.Is(err, content.ErrNotFound) {
		t.Fatalf("missing ID error=%v", err)
	}
	if _, err := s.GetAccountByEmail(ctx, " MISSING@EXAMPLE.TEST "); !errors.Is(err, content.ErrNotFound) {
		t.Fatalf("missing email error=%v", err)
	}
}

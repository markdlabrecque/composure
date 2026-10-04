package store_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/content"
	"github.com/markdlabrecque/composure/internal/store"
	"github.com/markdlabrecque/composure/internal/web"
)

const actorSessionCookieName = "__Host-composure_session"

type actorHTTPFixture struct {
	path       string
	repository *store.Store
	accountID  string
	credential string
}

func newActorHTTPFixture(t *testing.T, example *content.Snapshot) *actorHTTPFixture {
	t.Helper()
	ctx := context.Background()
	at := time.Date(2026, 10, 3, 18, 30, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "composure.db")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize(ctx, path, "actor-test-site", at, example); err != nil {
		t.Fatal(err)
	}
	repository, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	accountID, err := repository.CreateAccount(ctx, store.AccountDraft{
		Email: "actor@example.test", PasswordHash: "private-test-hash", IsEditor: true, State: "active",
	}, at)
	if err != nil {
		t.Fatal(err)
	}
	raw := bytes.Repeat([]byte{0xa2}, 32)
	digest := sha256.Sum256(raw)
	if _, err := repository.CreateSession(ctx, store.SessionDraft{AccountID: accountID, TokenDigest: digest[:]}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	fixture := &actorHTTPFixture{
		path: path, repository: repository, accountID: accountID,
		credential: base64.RawURLEncoding.EncodeToString(raw),
	}
	t.Cleanup(func() {
		if fixture.repository != nil {
			_ = fixture.repository.Close()
		}
	})
	return fixture
}

func (f *actorHTTPFixture) request(t *testing.T, target string, values url.Values, authenticated bool) *httptest.ResponseRecorder {
	t.Helper()
	requestValues := make(url.Values, len(values)+1)
	for key, entries := range values {
		requestValues[key] = append([]string(nil), entries...)
	}
	if authenticated {
		rawCredential, err := base64.RawURLEncoding.DecodeString(f.credential)
		if err != nil {
			t.Fatal(err)
		}
		mac := hmac.New(sha256.New, rawCredential)
		_, _ = mac.Write([]byte("composure:csrf:authenticated:v1"))
		requestValues.Set("csrf_token", base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
	}
	request := httptest.NewRequest(http.MethodPost, "https://127.0.0.1:8443"+target, strings.NewReader(requestValues.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if authenticated {
		request.Header.Set("Cookie", actorSessionCookieName+"="+f.credential)
	}
	response := httptest.NewRecorder()
	web.Handler(f.repository, "8443").ServeHTTP(response, request)
	return response
}

func (f *actorHTTPFixture) reopen(t *testing.T) {
	t.Helper()
	if err := f.repository.Close(); err != nil {
		t.Fatal(err)
	}
	f.repository = nil
	reopened, err := store.Open(context.Background(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	f.repository = reopened
}

func actorCreatedPageID(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	if response.Code != http.StatusSeeOther {
		t.Fatalf("create returned %d, want 303: %s", response.Code, response.Body.String())
	}
	location := response.Header().Get("Location")
	if !strings.HasPrefix(location, "/admin/pages/") || !strings.HasSuffix(location, "/edit") {
		t.Fatalf("create redirect = %q", location)
	}
	return strings.TrimSuffix(strings.TrimPrefix(location, "/admin/pages/"), "/edit")
}

func TestPageCreateStoresAuthenticatedAccountAsActorAcrossRestart(t *testing.T) {
	fixture := newActorHTTPFixture(t, nil)
	values := url.Values{
		"title": {"Account draft"}, "path": {"/account-draft"}, "body": {"body"},
		"actor": {"spoofed-form-actor"},
	}
	id := actorCreatedPageID(t, fixture.request(t, "/admin/pages", values, true))

	fixture.reopen(t)
	var createdBy, updatedBy string
	if err := fixture.repository.ActorAttributionForTest(context.Background(), id, 0, &createdBy, &updatedBy); err != nil {
		t.Fatal(err)
	}
	if createdBy != fixture.accountID || updatedBy != fixture.accountID {
		t.Fatalf("created actor = %q, updated actor = %q; want authenticated account %q", createdBy, updatedBy, fixture.accountID)
	}
}

func TestPageSaveStoresAuthenticatedAccountAsActorAcrossRestart(t *testing.T) {
	fixture := newActorHTTPFixture(t, nil)
	id, err := fixture.repository.CreateItem(context.Background(), content.ItemDraft{
		Title: "Original", Path: "/saved", Fields: map[string]string{"body": "before"},
	}, time.Date(2026, 10, 3, 18, 31, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	values := url.Values{
		"title": {"Saved by account"}, "path": {"/saved"}, "body": {"after"}, "draft_revision": {"1"},
		"actor": {"spoofed-form-actor"},
	}
	if response := fixture.request(t, "/admin/pages/"+id, values, true); response.Code != http.StatusSeeOther {
		t.Fatalf("save returned %d, want 303: %s", response.Code, response.Body.String())
	}

	fixture.reopen(t)
	var createdBy, updatedBy string
	if err := fixture.repository.ActorAttributionForTest(context.Background(), id, 0, &createdBy, &updatedBy); err != nil {
		t.Fatal(err)
	}
	if createdBy != "local-prototype" {
		t.Fatalf("save rewrote historical created actor to %q", createdBy)
	}
	if updatedBy != fixture.accountID {
		t.Fatalf("saved actor = %q, want authenticated account %q", updatedBy, fixture.accountID)
	}
}

func TestPagePublishStoresAuthenticatedAccountOnSnapshotAcrossRestart(t *testing.T) {
	fixture := newActorHTTPFixture(t, nil)
	id, err := fixture.repository.CreateItem(context.Background(), content.ItemDraft{
		Title: "Published by account", Path: "/published", Fields: map[string]string{"body": "public"},
	}, time.Date(2026, 10, 3, 18, 32, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	values := url.Values{"draft_revision": {"1"}, "actor": {"spoofed-form-actor"}}
	if response := fixture.request(t, "/admin/pages/"+id+"/publish", values, true); response.Code != http.StatusSeeOther {
		t.Fatalf("publish returned %d, want 303: %s", response.Code, response.Body.String())
	}

	fixture.reopen(t)
	var publishedBy string
	if err := fixture.repository.ActorAttributionForTest(context.Background(), id, 1, &publishedBy); err != nil {
		t.Fatal(err)
	}
	if publishedBy != fixture.accountID {
		t.Fatalf("published snapshot actor = %q, want authenticated account %q", publishedBy, fixture.accountID)
	}
}

func TestPageWritesRejectMissingPrincipalWithoutLocalActorFallback(t *testing.T) {
	fixture := newActorHTTPFixture(t, nil)
	before := 0
	if err := fixture.repository.ItemCountForTest(context.Background(), &before); err != nil {
		t.Fatal(err)
	}
	values := url.Values{
		"title": {"No principal"}, "path": {"/no-principal"}, "body": {"body"},
		"actor": {fixture.accountID},
	}
	response := fixture.request(t, "/admin/pages", values, false)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/admin/sign-in" {
		t.Fatalf("missing principal returned %d location %q, want sign-in redirect", response.Code, response.Header().Get("Location"))
	}
	fixture.reopen(t)
	var after int
	if err := fixture.repository.ItemCountForTest(context.Background(), &after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("missing principal changed item count from %d to %d", before, after)
	}
}

func TestHistoricalLocalPrototypeAttributionSurvivesAuthenticatedWrites(t *testing.T) {
	example := &content.Snapshot{
		ID: "example-snapshot", ItemID: "example-item", Title: "Historical example",
		Path: "/example", Fields: map[string]string{"body": "historical"},
	}
	fixture := newActorHTTPFixture(t, example)
	values := url.Values{"title": {"New page"}, "path": {"/new-page"}, "body": {"new"}}
	actorCreatedPageID(t, fixture.request(t, "/admin/pages", values, true))

	fixture.reopen(t)
	var createdBy, updatedBy, publishedBy string
	if err := fixture.repository.ActorAttributionForTest(context.Background(), example.ItemID, 1, &createdBy, &updatedBy, &publishedBy); err != nil {
		t.Fatal(err)
	}
	if createdBy != "local-prototype" || updatedBy != "local-prototype" || publishedBy != "local-prototype" {
		t.Fatalf("historical actors changed to created=%q updated=%q published=%q", createdBy, updatedBy, publishedBy)
	}
	page, err := fixture.repository.PublishedByPath(context.Background(), "/example")
	if err != nil || page.Title != example.Title || page.Fields["body"] != example.Fields["body"] {
		t.Fatalf("historical Page is no longer displayed from its stored snapshot: page=%+v err=%v", page, err)
	}
}

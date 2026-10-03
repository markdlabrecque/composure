package store_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/store"
	"github.com/markdlabrecque/composure/internal/web"
)

func TestInvitationRoutePersistsInjectedClockWithRealStore(t *testing.T) {
	ctx := context.Background()
	fixed := time.Date(2034, time.March, 4, 5, 6, 7, 890000000, time.FixedZone("test", -8*60*60))
	path := filepath.Join(t.TempDir(), "composure.db")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize(ctx, path, "invitation-clock-test", fixed, nil); err != nil {
		t.Fatal(err)
	}
	repository, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	accountID, err := repository.CreateAccount(ctx, store.AccountDraft{
		Email: "administrator@example.test", PasswordHash: "test", IsAdministrator: true, State: "active",
	}, fixed)
	if err != nil {
		t.Fatal(err)
	}
	rawCredential := []byte("01234567890123456789012345678901")
	credential := base64.RawURLEncoding.EncodeToString(rawCredential)
	digest := sha256.Sum256(rawCredential)
	if _, err := repository.CreateSession(ctx, store.SessionDraft{AccountID: accountID, TokenDigest: digest[:]}, fixed); err != nil {
		t.Fatal(err)
	}
	csrfMAC := hmac.New(sha256.New, rawCredential)
	_, _ = csrfMAC.Write([]byte("composure:csrf:authenticated:v1"))
	csrf := base64.RawURLEncoding.EncodeToString(csrfMAC.Sum(nil))
	form := url.Values{"email": {"clock@example.test"}, "csrf_token": {csrf}}.Encode()
	request := httptest.NewRequest(http.MethodPost, "https://127.0.0.1:8443/admin/invitations", strings.NewReader(form))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", "__Host-composure_session="+credential)
	response := httptest.NewRecorder()

	web.HandlerWithClock(repository, "8443", func() time.Time { return fixed }).ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("POST invitation returned %d, want 303", response.Code)
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var createdAt, expiresAt string
	if err := database.QueryRowContext(ctx, `
		SELECT created_at, expires_at
		FROM tokens
		WHERE purpose = 'invitation' AND email = 'clock@example.test'`).Scan(&createdAt, &expiresAt); err != nil {
		t.Fatalf("read persisted invitation metadata: %v", err)
	}
	wantCreatedAt := fixed.UTC().Format("2006-01-02T15:04:05.000Z")
	wantExpiresAt := fixed.Add(7 * 24 * time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")
	if createdAt != wantCreatedAt {
		t.Fatalf("persisted invitation created_at = %q, want injected clock %q", createdAt, wantCreatedAt)
	}
	if expiresAt != wantExpiresAt {
		t.Fatalf("persisted invitation expires_at = %q, want exact seven-day expiry %q", expiresAt, wantExpiresAt)
	}
}

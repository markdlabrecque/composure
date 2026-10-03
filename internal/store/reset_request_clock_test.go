package store_test

import (
	"context"
	"database/sql"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/store"
	"github.com/markdlabrecque/composure/internal/web"
	_ "modernc.org/sqlite"
)

var resetRequestCSRFPattern = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func TestResetRequestRoutePersistsInjectedClockWithRealStore(t *testing.T) {
	ctx := context.Background()
	fixed := time.Date(2034, time.March, 4, 5, 6, 7, 890000000, time.FixedZone("test", -8*60*60))
	path := filepath.Join(t.TempDir(), "composure.db")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize(ctx, path, "reset-request-clock-test", fixed, nil); err != nil {
		t.Fatal(err)
	}
	repository, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	accountID, err := repository.CreateAccount(ctx, store.AccountDraft{
		Email: "editor@example.test", PasswordHash: "test", IsEditor: true, State: "active",
	}, fixed)
	if err != nil {
		t.Fatal(err)
	}
	handler := web.HandlerWithClock(repository, "8443", func() time.Time { return fixed })
	getRequest := httptest.NewRequest(http.MethodGet, "https://127.0.0.1:8443/reset", nil)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("GET /reset returned %d, want 200", getResponse.Code)
	}
	cookies := getResponse.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("GET /reset set %d cookies, want one nonce cookie", len(cookies))
	}
	match := resetRequestCSRFPattern.FindStringSubmatch(getResponse.Body.String())
	if len(match) != 2 {
		t.Fatal("GET /reset did not render a CSRF token")
	}
	csrf := html.UnescapeString(match[1])
	form := url.Values{"email": {"editor@example.test"}, "csrf_token": {csrf}}.Encode()
	postRequest := httptest.NewRequest(http.MethodPost, "https://127.0.0.1:8443/reset", strings.NewReader(form))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRequest.AddCookie(cookies[0])
	postResponse := httptest.NewRecorder()
	handler.ServeHTTP(postResponse, postRequest)
	if postResponse.Code != http.StatusOK {
		t.Fatalf("POST /reset returned %d, want 200", postResponse.Code)
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var gotAccountID, purpose, createdAt, expiresAt string
	if err := database.QueryRowContext(ctx, `
		SELECT account_id, purpose, created_at, expires_at
		FROM tokens
		WHERE purpose = 'password_reset'`).Scan(&gotAccountID, &purpose, &createdAt, &expiresAt); err != nil {
		t.Fatalf("read persisted reset metadata: %v", err)
	}
	wantCreatedAt := fixed.UTC().Format("2006-01-02T15:04:05.000Z")
	wantExpiresAt := fixed.Add(time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")
	if gotAccountID != accountID || purpose != "password_reset" {
		t.Fatalf("persisted reset binding = (%q, %q), want (%q, password_reset)", gotAccountID, purpose, accountID)
	}
	if createdAt != wantCreatedAt {
		t.Fatalf("persisted reset created_at = %q, want injected UTC clock %q", createdAt, wantCreatedAt)
	}
	if expiresAt != wantExpiresAt {
		t.Fatalf("persisted reset expires_at = %q, want exact one-hour expiry %q", expiresAt, wantExpiresAt)
	}
}

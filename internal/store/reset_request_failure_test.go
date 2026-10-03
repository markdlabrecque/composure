package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
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

var resetRequestFailureCSRFPattern = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

type resetRequestFailureResponse struct {
	status       int
	body         string
	cacheControl string
	contentType  string
}

func TestResetRequestTokenInsertFailureIsPubliclyIndistinguishableFromUnknownAccount(t *testing.T) {
	ctx := context.Background()
	fixed := time.Date(2034, time.March, 4, 5, 6, 7, 890000000, time.FixedZone("test", -8*60*60))
	path := filepath.Join(t.TempDir(), "composure.db")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize(ctx, path, "reset-request-failure-test", fixed, nil); err != nil {
		t.Fatal(err)
	}
	repository, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	if _, err := repository.CreateAccount(ctx, store.AccountDraft{
		Email: "known@example.test", PasswordHash: "test", IsEditor: true, State: "active",
	}, fixed); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.GetAccountByEmail(ctx, "known@example.test"); err != nil {
		t.Fatalf("active account lookup failed before token insertion fault: %v", err)
	}

	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.ExecContext(ctx, `
		CREATE TRIGGER reset_request_token_insert_failure
		BEFORE INSERT ON tokens
		BEGIN
			SELECT RAISE(ABORT, 'reset request token insert failure');
		END`); err != nil {
		t.Fatal(err)
	}
	tokenCount := func() int {
		t.Helper()
		var count int
		if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM tokens`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	if got := tokenCount(); got != 0 {
		t.Fatalf("tokens before requests = %d, want 0", got)
	}

	handler := web.HandlerWithClock(repository, "8443", func() time.Time { return fixed })
	session := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x6a}, 32))
	post := func(email string) resetRequestFailureResponse {
		t.Helper()
		getRequest := httptest.NewRequest(http.MethodGet, "https://127.0.0.1:8443/reset", nil)
		getRequest.Header.Set("Cookie", "__Host-composure_session="+session)
		getResponse := httptest.NewRecorder()
		handler.ServeHTTP(getResponse, getRequest)
		if getResponse.Code != http.StatusOK {
			t.Fatalf("GET /reset returned %d, want 200", getResponse.Code)
		}
		cookies := getResponse.Result().Cookies()
		var nonceCookie *http.Cookie
		for _, cookie := range cookies {
			if cookie.Name == "__Host-composure_csrf_nonce" {
				nonceCookie = cookie
			}
		}
		if nonceCookie == nil {
			t.Fatalf("GET /reset did not set nonce cookie among %d cookies", len(cookies))
		}
		match := resetRequestFailureCSRFPattern.FindStringSubmatch(getResponse.Body.String())
		if len(match) != 2 {
			t.Fatal("GET /reset did not render a CSRF token")
		}
		form := url.Values{"email": {email}, "csrf_token": {html.UnescapeString(match[1])}}.Encode()
		postRequest := httptest.NewRequest(http.MethodPost, "https://127.0.0.1:8443/reset", strings.NewReader(form))
		postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		postRequest.Header.Set("Cookie", "__Host-composure_session="+session)
		postRequest.AddCookie(nonceCookie)
		postResponse := httptest.NewRecorder()
		handler.ServeHTTP(postResponse, postRequest)
		return resetRequestFailureResponse{
			status:       postResponse.Code,
			body:         postResponse.Body.String(),
			cacheControl: postResponse.Header().Get("Cache-Control"),
			contentType:  postResponse.Header().Get("Content-Type"),
		}
	}

	known := post("known@example.test")
	if got := tokenCount(); got != 0 {
		t.Fatalf("tokens after failed known-account insertion = %d, want 0", got)
	}
	unknown := post("unknown@example.test")
	if got := tokenCount(); got != 0 {
		t.Fatalf("tokens after unknown-account request = %d, want 0", got)
	}
	if known != unknown {
		t.Fatalf("known failed-write and unknown responses differ: known=%+v unknown=%+v", known, unknown)
	}
}

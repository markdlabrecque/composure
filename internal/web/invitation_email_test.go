package web

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/config"
	composuremail "github.com/markdlabrecque/composure/internal/mail"
	"github.com/markdlabrecque/composure/internal/mail/mailtest"
	"github.com/markdlabrecque/composure/internal/store"
)

type invitationDeliverySenderForTest func(context.Context, config.SMTP, composuremail.Message) error

type invitationDeliveryForTest struct {
	publicURL string
	relay     config.SMTP
	send      invitationDeliverySenderForTest
	enabled   bool
}

type invitationEmailRepositoryForTest struct {
	*invitationRepositoryForTest
	delivery invitationDeliveryForTest
	issueErr error
}

func (r *invitationEmailRepositoryForTest) InvitationDelivery() (string, config.SMTP, func(context.Context, config.SMTP, composuremail.Message) error, bool) {
	return r.delivery.publicURL, r.delivery.relay, r.delivery.send, r.delivery.enabled
}

func (r *invitationEmailRepositoryForTest) IssueToken(ctx context.Context, draft store.TokenDraft, at time.Time) (store.Token, string, error) {
	if r.issueErr != nil {
		return store.Token{}, "", r.issueErr
	}
	return r.invitationRepositoryForTest.IssueToken(ctx, draft, at)
}

func invitationEmailFixtureForTest(t *testing.T) (invitationFixtureForTest, *invitationEmailRepositoryForTest, *mailtest.Server) {
	return invitationEmailFixtureWithRolesForTest(t, true, false)
}

func invitationEmailFixtureWithRolesForTest(t *testing.T, administrator, editor bool) (invitationFixtureForTest, *invitationEmailRepositoryForTest, *mailtest.Server) {
	t.Helper()
	f := newInvitationFixtureForTest(t, administrator, editor)
	capture := mailtest.New(t)
	host, portText, err := net.SplitHostPort(capture.Addr())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	repository := &invitationEmailRepositoryForTest{
		invitationRepositoryForTest: f.repository,
		delivery: invitationDeliveryForTest{
			publicURL: "https://public.example.test",
			relay:     config.SMTP{Host: host, Port: port, From: "sender@example.test", TLS: "plaintext"},
			send: func(ctx context.Context, relay config.SMTP, message composuremail.Message) error {
				return composuremail.Send(ctx, relay, message)
			},
			enabled: true,
		},
	}
	f.repository = repository.invitationRepositoryForTest
	return f, repository, capture
}

func invitationEmailServeForTest(f invitationFixtureForTest, repository *invitationEmailRepositoryForTest, request *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	HandlerWithClock(repository, "8443", func() time.Time { return f.now }).ServeHTTP(response, request)
	return response
}

func TestInvitationEmailUsesTrustedPublicURLAndRealSMTP(t *testing.T) {
	f, repository, capture := invitationEmailFixtureForTest(t)
	request := invitationRequestForTest(http.MethodPost, "/admin/invitations", invitationValuesForTest(t, f.credential, "  Invited.User+Tag@EXAMPLE.TEST  "), f.credential)
	request.Header.Set("Forwarded", "host=forwarded-attacker.example.test;proto=http")
	request.Header.Set("X-Forwarded-Host", "legacy-attacker.example.test")
	request.Header.Set("X-Forwarded-Proto", "http")

	response := invitationEmailServeForTest(f, repository, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("configured invitation returned %d, want 303", response.Code)
	}
	if len(repository.issueCalls) != 1 {
		t.Fatalf("configured invitation issued %d credentials, want one", len(repository.issueCalls))
	}
	messages := capture.Messages()
	if len(messages) != 1 {
		t.Fatalf("configured invitation delivered %d messages, want one", len(messages))
	}
	issued := repository.issueCalls[0]
	wantLink := "https://public.example.test/invite/" + issued.raw
	message := messages[0]
	if message.From != "sender@example.test" || len(message.To) != 1 || message.To[0] != "invited.user+tag@example.test" {
		t.Fatal("invitation SMTP envelope did not use the configured sender and canonical recipient")
	}
	data := string(message.Data)
	if strings.Count(data, wantLink) != 1 {
		t.Fatal("invitation message must contain exactly one trusted purpose-path link")
	}
	for _, poisoned := range []string{"attacker.example.test", "forwarded-attacker.example.test", "legacy-attacker.example.test"} {
		if strings.Contains(data, poisoned) {
			t.Fatal("invitation link used request-controlled host data")
		}
	}
	if strings.Contains(wantLink, "?") || strings.Contains(wantLink, "#") {
		t.Fatal("invitation credential must be a path segment")
	}
	if strings.Contains(response.Body.String(), issued.raw) || headerContainsInvitationSecretForTest(response.Header(), issued.raw) {
		t.Fatal("HTTP response exposed the raw invitation credential")
	}
	if bytes.Contains(invitationDatabaseBytesForTest(t, f.path), []byte(issued.raw)) {
		t.Fatal("database persisted the raw invitation credential")
	}
}

func TestInvitationEmailRejectsBeforeDelivery(t *testing.T) {
	for _, tc := range []struct {
		name       string
		email      string
		credential bool
		csrf       bool
		editor     bool
		deactivate bool
		issueErr   bool
		poisonHost bool
		wantStatus int
	}{
		{name: "invalid recipient", email: "not an address", credential: true, csrf: true, wantStatus: http.StatusUnprocessableEntity},
		{name: "unauthenticated", email: "denied@example.test", csrf: true, wantStatus: http.StatusSeeOther},
		{name: "editor", email: "denied@example.test", credential: true, csrf: true, editor: true, wantStatus: http.StatusForbidden},
		{name: "deactivated", email: "denied@example.test", credential: true, csrf: true, deactivate: true, wantStatus: http.StatusSeeOther},
		{name: "invalid csrf", email: "denied@example.test", credential: true, wantStatus: http.StatusForbidden},
		{name: "poisoned host", email: "denied@example.test", credential: true, csrf: true, poisonHost: true, wantStatus: http.StatusBadRequest},
		{name: "issuance failure", email: "denied@example.test", credential: true, csrf: true, issueErr: true, wantStatus: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, repository, capture := invitationEmailFixtureWithRolesForTest(t, !tc.editor, tc.editor)
			if tc.deactivate {
				if _, err := repository.CreateAccount(context.Background(), store.AccountDraft{Email: "remaining@example.test", PasswordHash: "test", IsAdministrator: true, State: "active"}, f.now); err != nil {
					t.Fatal(err)
				}
				if err := repository.DeactivateAccount(context.Background(), f.accountID, f.now); err != nil {
					t.Fatal(err)
				}
			}
			if tc.issueErr {
				repository.issueErr = errors.New("injected issuance failure")
			}
			values := url.Values{"email": {tc.email}, "csrf_token": {"invalid"}}
			if tc.csrf {
				values.Set("csrf_token", guardCSRFToken(t, f.credential))
			}
			credential := ""
			if tc.credential {
				credential = f.credential
			}
			request := invitationRequestForTest(http.MethodPost, "/admin/invitations", values, credential)
			if tc.poisonHost {
				request.Host = "attacker.example.test"
			}
			response := invitationEmailServeForTest(f, repository, request)
			if response.Code != tc.wantStatus {
				t.Fatalf("rejected invitation returned %d, want %d", response.Code, tc.wantStatus)
			}
			if len(capture.Messages()) != 0 {
				t.Fatal("rejected invitation delivered mail")
			}
			if len(repository.issueCalls) != 0 {
				t.Fatal("rejected invitation issued a credential")
			}
		})
	}
}

func TestInvitationEmailDeliveryFailureIsBoundedAndPrivate(t *testing.T) {
	f, repository, capture := invitationEmailFixtureForTest(t)
	repository.delivery.send = func(context.Context, config.SMTP, composuremail.Message) error {
		return errors.New("injected SMTP failure")
	}
	started := time.Now()
	response := invitationEmailServeForTest(f, repository, invitationRequestForTest(http.MethodPost, "/admin/invitations", invitationValuesForTest(t, f.credential, "private-recipient@example.test"), f.credential))
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatal("SMTP failure handling exceeded its deterministic bound")
	}
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("SMTP failure returned %d, want 500", response.Code)
	}
	if len(repository.issueCalls) != 1 {
		t.Fatalf("SMTP failure retained %d issued credentials, want one", len(repository.issueCalls))
	}
	if len(capture.Messages()) != 0 {
		t.Fatal("failed SMTP delivery was reported by the capture server")
	}
	issued := repository.issueCalls[0]
	for _, private := range []string{issued.raw, "private-recipient@example.test", repository.delivery.relay.Host} {
		if strings.Contains(response.Body.String(), private) || headerContainsInvitationSecretForTest(response.Header(), private) {
			t.Fatal("SMTP failure response exposed private delivery data")
		}
	}
}

func headerContainsInvitationSecretForTest(header http.Header, secret string) bool {
	for name, values := range header {
		if strings.Contains(name, secret) {
			return true
		}
		for _, value := range values {
			if strings.Contains(value, secret) {
				return true
			}
		}
	}
	return false
}

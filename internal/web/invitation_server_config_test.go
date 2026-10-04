package web

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/markdlabrecque/composure/internal/mail/mailtest"
)

var invitationSMTPEnvironmentKeysForTest = []string{
	"COMPOSURE_SMTP_HOST",
	"COMPOSURE_SMTP_PORT",
	"COMPOSURE_SMTP_FROM",
	"COMPOSURE_SMTP_USERNAME",
	"COMPOSURE_SMTP_PASSWORD",
	"COMPOSURE_SMTP_TLS",
}

func setInvitationServerEnvironmentForTest(t *testing.T, capture *mailtest.Server, publicURL string) {
	t.Helper()
	for _, key := range invitationSMTPEnvironmentKeysForTest {
		t.Setenv(key, "")
	}
	t.Setenv("COMPOSURE_PUBLIC_URL", publicURL)
	if capture == nil {
		return
	}
	host, port, err := net.SplitHostPort(capture.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMPOSURE_SMTP_HOST", host)
	t.Setenv("COMPOSURE_SMTP_PORT", port)
	t.Setenv("COMPOSURE_SMTP_FROM", "sender@example.test")
	t.Setenv("COMPOSURE_SMTP_TLS", "plaintext")
}

func serveInvitationThroughConfiguredServerForTest(t *testing.T, f invitationFixtureForTest, repository *invitationRepositoryForTest, email string) *httptest.ResponseRecorder {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	request := invitationRequestForTest(http.MethodPost, "/admin/invitations", invitationValuesForTest(t, f.credential, email), f.credential)
	request.URL.Host = listener.Addr().String()
	request.Host = listener.Addr().String()
	response := httptest.NewRecorder()
	Server(repository, repository, listener).Handler.ServeHTTP(response, request)
	return response
}

func TestConfiguredServerDeliversInvitationFromEnvironment(t *testing.T) {
	f := newInvitationFixtureForTest(t, true, false)
	capture := mailtest.New(t)
	setInvitationServerEnvironmentForTest(t, capture, "https://public.example.test/")

	response := serveInvitationThroughConfiguredServerForTest(t, f, f.repository, "server@example.test")
	if response.Code != http.StatusSeeOther {
		t.Fatalf("configured Server invitation returned %d, want 303", response.Code)
	}
	if len(f.repository.issueCalls) != 1 {
		t.Fatalf("configured Server issued %d credentials, want one", len(f.repository.issueCalls))
	}
	messages := capture.Messages()
	if len(messages) != 1 {
		t.Fatalf("configured Server delivered %d messages, want one", len(messages))
	}
	wantLink := "https://public.example.test/invite/" + f.repository.issueCalls[0].raw
	if len(messages[0].To) != 1 || messages[0].To[0] != "server@example.test" || !containsInvitationLinkForTest(messages[0].Data, wantLink) {
		t.Fatal("configured Server did not deliver the trusted invitation link to the canonical recipient")
	}
}

func TestConfiguredServerRejectsInvalidPublicURLWithoutIssuingOrSending(t *testing.T) {
	invalid := []struct {
		name, value string
	}{
		{name: "relative", value: "public.example.test"},
		{name: "unsupported scheme", value: "ftp://public.example.test"},
		{name: "userinfo", value: "https://operator:secret@public.example.test"},
		{name: "query", value: "https://public.example.test?tenant=private"},
		{name: "fragment", value: "https://public.example.test#private"},
		{name: "control", value: "https://public.example.test/\nprivate"},
		{name: "base path", value: "https://public.example.test/base"},
		{name: "base path with slash", value: "https://public.example.test/base/"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			f := newInvitationFixtureForTest(t, true, false)
			capture := mailtest.New(t)
			setInvitationServerEnvironmentForTest(t, capture, tc.value)
			response := serveInvitationThroughConfiguredServerForTest(t, f, f.repository, "private-recipient@example.test")
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("invalid public URL returned %d, want 500", response.Code)
			}
			if len(f.repository.issueCalls) != 0 {
				t.Fatal("invalid public URL issued an invitation credential")
			}
			if len(capture.Messages()) != 0 {
				t.Fatal("invalid public URL delivered mail")
			}
			for _, private := range []string{tc.value, "private-recipient@example.test"} {
				if private != "" && (containsInvitationLinkForTest(response.Body.Bytes(), private) || headerContainsInvitationSecretForTest(response.Header(), private)) {
					t.Fatal("invalid configuration diagnostic exposed an input value")
				}
			}
		})
	}
}

func TestServerWithoutInvitationDeliveryConfigurationRetainsTokenOnlyBehavior(t *testing.T) {
	f := newInvitationFixtureForTest(t, true, false)
	setInvitationServerEnvironmentForTest(t, nil, "")
	response := serveInvitationThroughConfiguredServerForTest(t, f, f.repository, "disabled@example.test")
	if response.Code != http.StatusSeeOther {
		t.Fatalf("disabled delivery invitation returned %d, want 303", response.Code)
	}
	if len(f.repository.issueCalls) != 1 {
		t.Fatalf("disabled delivery issued %d credentials, want one", len(f.repository.issueCalls))
	}
}

func TestConfiguredServerRejectsInvalidSMTPWithoutIssuing(t *testing.T) {
	f := newInvitationFixtureForTest(t, true, false)
	setInvitationServerEnvironmentForTest(t, nil, "https://public.example.test")
	t.Setenv("COMPOSURE_SMTP_HOST", "smtp-secret.invalid")
	t.Setenv("COMPOSURE_SMTP_PORT", strconv.Itoa(70000))
	t.Setenv("COMPOSURE_SMTP_FROM", "private-sender@example.test")
	response := serveInvitationThroughConfiguredServerForTest(t, f, f.repository, "private-recipient@example.test")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("invalid SMTP configuration returned %d, want 500", response.Code)
	}
	if len(f.repository.issueCalls) != 0 {
		t.Fatal("invalid SMTP configuration issued an invitation credential")
	}
	for _, private := range []string{"smtp-secret.invalid", "private-sender@example.test", "private-recipient@example.test"} {
		if containsInvitationLinkForTest(response.Body.Bytes(), private) || headerContainsInvitationSecretForTest(response.Header(), private) {
			t.Fatal("invalid SMTP diagnostic exposed a configuration value")
		}
	}
}

func containsInvitationLinkForTest(haystack []byte, needle string) bool {
	return len(needle) != 0 && stringContainsForInvitationTest(string(haystack), needle)
}

func stringContainsForInvitationTest(haystack, needle string) bool {
	if len(needle) == 0 {
		return false
	}
	for start := 0; start+len(needle) <= len(haystack); start++ {
		if haystack[start:start+len(needle)] == needle {
			return true
		}
	}
	return false
}

package mail_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/config"
	composuremail "github.com/markdlabrecque/composure/internal/mail"
	"github.com/markdlabrecque/composure/internal/mail/mailtest"
)

const sendTimeout = 2 * time.Second

func TestSendDeliversPlainTextThroughLoopbackCapture(t *testing.T) {
	server := mailtest.New(t)
	host, port := splitAddr(t, server.Addr())
	relay := config.SMTP{Host: host, Port: port, From: "sender@example.test", TLS: "plaintext"}
	message := composuremail.Message{
		To:      "recipient@example.test",
		Subject: "Welcome to Composure",
		Body:    "first line\nsecond line\n.dot-prefixed line",
	}

	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if err := composuremail.Send(ctx, relay, message); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	received, err := server.WaitForMessage(ctx)
	if err != nil {
		t.Fatalf("WaitForMessage() error = %v", err)
	}
	if received.From != relay.From {
		t.Errorf("envelope from = %q, want %q", received.From, relay.From)
	}
	if len(received.To) != 1 || received.To[0] != message.To {
		t.Errorf("envelope recipients = %q, want [%q]", received.To, message.To)
	}
	data := string(received.Data)
	for _, want := range []string{
		"From: sender@example.test\r\n",
		"To: recipient@example.test\r\n",
		"Subject: Welcome to Composure\r\n",
		"Content-Type: text/plain; charset=UTF-8\r\n",
		"\r\nfirst line\r\nsecond line\r\n.dot-prefixed line",
	} {
		if !strings.Contains(data, want) {
			t.Errorf("message DATA does not contain %q; got %q", want, data)
		}
	}
}

func TestSendRejectsInvalidInputsBeforeConnecting(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*config.SMTP, *composuremail.Message)
		secrets []string
	}{
		{name: "disabled relay", mutate: func(relay *config.SMTP, _ *composuremail.Message) { *relay = config.SMTP{} }},
		{name: "missing port", mutate: func(relay *config.SMTP, _ *composuremail.Message) { relay.Port = 0 }},
		{name: "invalid host", mutate: func(relay *config.SMTP, _ *composuremail.Message) {
			relay.Host = "localhost\r\nMAIL FROM:<stolen@example.test>"
		}, secrets: []string{"stolen@example.test"}},
		{name: "invalid TLS mode", mutate: func(relay *config.SMTP, _ *composuremail.Message) { relay.TLS = "sometimes" }},
		{name: "unpaired username", mutate: func(relay *config.SMTP, _ *composuremail.Message) { relay.Username = "relay-user" }},
		{name: "sender injection", mutate: func(relay *config.SMTP, _ *composuremail.Message) {
			relay.From = "sender@example.test\r\nBcc: stolen@example.test"
		}, secrets: []string{"stolen@example.test"}},
		{name: "recipient injection", mutate: func(_ *config.SMTP, message *composuremail.Message) {
			message.To = "recipient@example.test\r\nBcc: stolen@example.test"
		}, secrets: []string{"recipient@example.test", "stolen@example.test"}},
		{name: "malformed recipient", mutate: func(_ *config.SMTP, message *composuremail.Message) { message.To = "not an address" }, secrets: []string{"not an address"}},
		{name: "subject injection", mutate: func(_ *config.SMTP, message *composuremail.Message) {
			message.Subject = "hello\r\nBcc: stolen@example.test"
		}, secrets: []string{"stolen@example.test"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := newSMTPServer(t, smtpBehavior{})
			host, port := splitAddr(t, server.addr())
			relay := config.SMTP{Host: host, Port: port, From: "sender@example.test", TLS: "plaintext"}
			message := composuremail.Message{To: "recipient@example.test", Subject: "subject", Body: "body-secret"}
			tc.mutate(&relay, &message)

			err := composuremail.Send(context.Background(), relay, message)
			if err == nil {
				t.Fatal("Send() error = nil, want validation failure")
			}
			if got := server.acceptedCount(); got != 0 {
				t.Fatalf("connections accepted = %d, want 0", got)
			}
			assertRedacted(t, err, append(tc.secrets, relay.Password, message.Body)...)
		})
	}
}

func TestSendRequiresWorkingSTARTTLS(t *testing.T) {
	tests := []struct {
		name     string
		behavior smtpBehavior
	}{
		{name: "extension missing", behavior: smtpBehavior{}},
		{name: "upgrade rejected", behavior: smtpBehavior{advertiseSTARTTLS: true, failAt: "starttls"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := newSMTPServer(t, tc.behavior)
			err := sendToServer(context.Background(), server, config.SMTP{TLS: "starttls"}, composuremail.Message{To: "recipient-secret@example.test", Subject: "subject-secret", Body: "body-secret"})
			if err == nil {
				t.Fatal("Send() error = nil, want STARTTLS failure")
			}
			assertRedacted(t, err, server.addr(), "recipient-secret@example.test", "subject-secret", "body-secret")
			server.waitClosed(t)
		})
	}
}

func TestSendAuthenticatesOverVerifiedSTARTTLS(t *testing.T) {
	server := newSMTPServer(t, smtpBehavior{advertiseSTARTTLS: true, tls: true, authUser: "relay-user", authPassword: " opaque\npassword "})
	relay := config.SMTP{TLS: "starttls", Username: "relay-user", Password: " opaque\npassword "}
	message := composuremail.Message{To: "recipient@example.test", Subject: "TLS delivery", Body: "secure body"}

	if err := sendToServerWithRoots(context.Background(), server, relay, message); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	server.waitClosed(t)
	if !server.authenticated() {
		t.Fatal("server did not observe successful authentication")
	}
	if !server.receivedData() {
		t.Fatal("server did not receive message DATA")
	}
}

func TestSendSupportsVerifiedImplicitTLS(t *testing.T) {
	server := newSMTPServer(t, smtpBehavior{implicitTLS: true, tls: true})
	if err := sendToServerWithRoots(context.Background(), server, config.SMTP{TLS: "implicit"}, composuremail.Message{To: "recipient@example.test", Subject: "subject", Body: "body"}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	server.waitClosed(t)
}

func TestSendRejectsUntrustedAndWrongHostnameCertificates(t *testing.T) {
	t.Run("untrusted certificate", func(t *testing.T) {
		server := newSMTPServer(t, smtpBehavior{implicitTLS: true, tls: true})
		err := sendToServer(context.Background(), server, config.SMTP{TLS: "implicit"}, composuremail.Message{To: "recipient@example.test", Subject: "subject", Body: "body"})
		if err == nil {
			t.Fatal("Send() error = nil, want certificate rejection")
		}
		server.waitClosed(t)
	})

	t.Run("hostname mismatch", func(t *testing.T) {
		server := newSMTPServer(t, smtpBehavior{implicitTLS: true, tls: true})
		host, port := splitAddr(t, server.addr())
		relay := config.SMTP{Host: host, Port: port, From: "sender@example.test", TLS: "implicit"}
		err := composuremail.Send(context.Background(), relay, composuremail.Message{To: "recipient@example.test", Subject: "subject", Body: "body"}, composuremail.WithRootCAs(server.roots))
		if err == nil {
			t.Fatal("Send() error = nil, want hostname rejection")
		}
		server.waitClosed(t)
	})
}

func TestSendReportsSMTPStageFailuresAndClosesConnection(t *testing.T) {
	for _, stage := range []string{"auth", "mail", "recipient", "data", "data-body"} {
		t.Run(stage, func(t *testing.T) {
			behavior := smtpBehavior{advertiseSTARTTLS: true, tls: true, failAt: stage}
			relay := config.SMTP{TLS: "starttls"}
			if stage == "auth" {
				behavior.authUser, behavior.authPassword = "relay-user", "password-secret"
				relay.Username, relay.Password = "relay-user", "wrong-password-secret"
			}
			server := newSMTPServer(t, behavior)
			message := composuremail.Message{To: "recipient-secret@example.test", Subject: "subject-secret", Body: "body-secret"}
			err := sendToServerWithRoots(context.Background(), server, relay, message)
			if err == nil {
				t.Fatalf("Send() error = nil, want %s failure", stage)
			}
			assertRedacted(t, err, server.addr(), relay.Username, relay.Password, relay.From, message.To, message.Subject, message.Body)
			server.waitClosed(t)
		})
	}
}

func TestSendDoesNotLogSensitiveValues(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	server := newSMTPServer(t, smtpBehavior{failAt: "recipient"})
	message := composuremail.Message{To: "recipient-secret@example.test", Subject: "subject-secret", Body: "body-secret"}
	err := sendToServer(context.Background(), server, config.SMTP{TLS: "plaintext", Password: "password-secret", Username: "relay-user"}, message)
	if err == nil {
		t.Fatal("Send() error = nil, want recipient failure")
	}
	for _, secret := range []string{server.addr(), "sender-secret@example.test", "recipient-secret@example.test", "subject-secret", "body-secret", "relay-user", "password-secret"} {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("log output exposes sensitive value %q: %q", secret, logs.String())
		}
	}
}

func TestSendCancellationIsBoundedAndClosesConnection(t *testing.T) {
	server := newSMTPServer(t, smtpBehavior{holdGreeting: true})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sendToServer(ctx, server, config.SMTP{TLS: "plaintext"}, composuremail.Message{To: "recipient@example.test", Subject: "subject", Body: "body"})
	}()
	server.waitAccepted(t)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Send() error = %v, want context.Canceled", err)
		}
	case <-time.After(sendTimeout):
		t.Fatal("Send() did not return promptly after cancellation")
	}
	server.waitClosed(t)
}

func sendToServer(ctx context.Context, server *smtpServer, relay config.SMTP, message composuremail.Message) error {
	host, port := splitAddr(server.t, server.addr())
	relay.Host, relay.Port, relay.From = host, port, "sender-secret@example.test"
	return composuremail.Send(ctx, relay, message)
}

func sendToServerWithRoots(ctx context.Context, server *smtpServer, relay config.SMTP, message composuremail.Message) error {
	host, port := splitAddr(server.t, server.addr())
	relay.Host, relay.Port, relay.From = "localhost", port, "sender-secret@example.test"
	_ = host
	return composuremail.Send(ctx, relay, message, composuremail.WithRootCAs(server.roots))
}

func assertRedacted(t *testing.T, err error, secrets ...string) {
	t.Helper()
	text := err.Error()
	for _, secret := range secrets {
		if secret != "" && strings.Contains(text, secret) {
			t.Errorf("error exposes sensitive value %q: %q", secret, text)
		}
	}
}

func splitAddr(t testing.TB, addr string) (string, int) {
	t.Helper()
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split address: %v", err)
	}
	var port int
	if _, err := fmt.Sscanf(portText, "%d", &port); err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return host, port
}

type smtpBehavior struct {
	advertiseSTARTTLS bool
	implicitTLS       bool
	tls               bool
	holdGreeting      bool
	failAt            string
	authUser          string
	authPassword      string
}

type smtpServer struct {
	t        *testing.T
	listener net.Listener
	behavior smtpBehavior
	cert     tls.Certificate
	roots    *x509.CertPool

	mu          sync.Mutex
	accepted    int
	active      int
	authed      bool
	gotData     bool
	acceptedCh  chan struct{}
	allClosedCh chan struct{}
	closeOnce   sync.Once
}

func newSMTPServer(t *testing.T, behavior smtpBehavior) *smtpServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	cert, roots := testCertificate(t)
	server := &smtpServer{t: t, listener: listener, behavior: behavior, cert: cert, roots: roots, acceptedCh: make(chan struct{}), allClosedCh: make(chan struct{})}
	go server.serve()
	t.Cleanup(server.close)
	return server
}

func (s *smtpServer) addr() string { return s.listener.Addr().String() }

func (s *smtpServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.accepted++
		s.active++
		if s.accepted == 1 {
			close(s.acceptedCh)
		}
		s.mu.Unlock()
		go s.handle(conn)
	}
}

func (s *smtpServer) handle(raw net.Conn) {
	defer func() {
		_ = raw.Close()
		s.mu.Lock()
		s.active--
		if s.active == 0 {
			select {
			case <-s.allClosedCh:
			default:
				close(s.allClosedCh)
			}
		}
		s.mu.Unlock()
	}()
	if s.behavior.holdGreeting {
		_, _ = io.Copy(io.Discard, raw)
		return
	}
	conn := raw
	if s.behavior.implicitTLS {
		conn = tls.Server(raw, &tls.Config{Certificates: []tls.Certificate{s.cert}, MinVersion: tls.VersionTLS12})
	}
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	write := func(line string) bool {
		_, err := io.WriteString(writer, line)
		return err == nil && writer.Flush() == nil
	}
	if !write("220 localhost ESMTP test\r\n") {
		return
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(command)
		switch {
		case strings.HasPrefix(upper, "EHLO "):
			response := "250-localhost\r\n"
			if s.behavior.advertiseSTARTTLS && !s.behavior.implicitTLS {
				response += "250-STARTTLS\r\n"
			}
			if s.behavior.authUser != "" && (s.behavior.implicitTLS || isTLSConn(conn)) {
				response += "250-AUTH PLAIN\r\n"
			}
			if !write(response + "250 OK\r\n") {
				return
			}
		case upper == "STARTTLS":
			if s.behavior.failAt == "starttls" {
				write("454 TLS unavailable\r\n")
				return
			}
			if !write("220 ready for TLS\r\n") {
				return
			}
			tlsConn := tls.Server(raw, &tls.Config{Certificates: []tls.Certificate{s.cert}, MinVersion: tls.VersionTLS12})
			conn, reader, writer = tlsConn, bufio.NewReader(tlsConn), bufio.NewWriter(tlsConn)
		case strings.HasPrefix(upper, "AUTH PLAIN"):
			parts := strings.Fields(command)
			if len(parts) < 3 {
				if !write("334 \r\n") {
					return
				}
				line, err = reader.ReadString('\n')
				if err != nil {
					return
				}
				parts = []string{"AUTH", "PLAIN", strings.TrimSpace(line)}
			}
			decoded, err := base64.StdEncoding.DecodeString(parts[2])
			want := "\x00" + s.behavior.authUser + "\x00" + s.behavior.authPassword
			if err != nil || string(decoded) != want || s.behavior.failAt == "auth" {
				write("535 authentication failed\r\n")
				continue
			}
			s.mu.Lock()
			s.authed = true
			s.mu.Unlock()
			if !write("235 authenticated\r\n") {
				return
			}
		case strings.HasPrefix(upper, "MAIL FROM:"):
			if s.behavior.authUser != "" && !s.authenticated() {
				if !write("530 authentication required\r\n") {
					return
				}
				continue
			}
			if s.behavior.failAt == "mail" {
				if !write("550 sender rejected\r\n") {
					return
				}
				continue
			}
			if !write("250 sender OK\r\n") {
				return
			}
		case strings.HasPrefix(upper, "RCPT TO:"):
			if s.behavior.failAt == "recipient" {
				if !write("550 recipient rejected\r\n") {
					return
				}
				continue
			}
			if !write("250 recipient OK\r\n") {
				return
			}
		case upper == "DATA":
			if s.behavior.failAt == "data" {
				if !write("554 data rejected\r\n") {
					return
				}
				continue
			}
			if !write("354 send data\r\n") {
				return
			}
			for {
				line, err = reader.ReadString('\n')
				if err != nil {
					return
				}
				if line == ".\r\n" {
					break
				}
			}
			if s.behavior.failAt == "data-body" {
				if !write("554 body rejected\r\n") {
					return
				}
				continue
			}
			s.mu.Lock()
			s.gotData = true
			s.mu.Unlock()
			if !write("250 accepted\r\n") {
				return
			}
		case upper == "QUIT":
			write("221 bye\r\n")
			return
		default:
			if !write("500 unsupported\r\n") {
				return
			}
		}
	}
}

func isTLSConn(conn net.Conn) bool { _, ok := conn.(*tls.Conn); return ok }

func (s *smtpServer) acceptedCount() int  { s.mu.Lock(); defer s.mu.Unlock(); return s.accepted }
func (s *smtpServer) authenticated() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.authed }
func (s *smtpServer) receivedData() bool  { s.mu.Lock(); defer s.mu.Unlock(); return s.gotData }

func (s *smtpServer) waitAccepted(t *testing.T) {
	t.Helper()
	select {
	case <-s.acceptedCh:
	case <-time.After(sendTimeout):
		t.Fatal("server did not accept connection")
	}
}

func (s *smtpServer) waitClosed(t *testing.T) {
	t.Helper()
	select {
	case <-s.allClosedCh:
	case <-time.After(sendTimeout):
		t.Fatal("client connection was not closed")
	}
}

func (s *smtpServer) close() {
	s.closeOnce.Do(func() { _ = s.listener.Close() })
}

func testCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "localhost"},
		DNSNames:              []string{"localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("parse key pair: %v", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("append test root certificate")
	}
	return cert, roots
}

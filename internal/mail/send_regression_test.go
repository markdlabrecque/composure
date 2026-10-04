package mail_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/config"
	composuremail "github.com/markdlabrecque/composure/internal/mail"
)

func TestSendAllowsAuthenticatedPlaintextOnLoopback(t *testing.T) {
	for _, tc := range []struct {
		name       string
		listenHost string
		relayHost  string
	}{
		{name: "localhost", listenHost: "127.0.0.1", relayHost: "localhost"},
		{name: "literal IPv4", listenHost: "127.0.0.1", relayHost: "127.0.0.1"},
		{name: "literal IPv6", listenHost: "::1", relayHost: "::1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := newRegressionSMTPServer(t, tc.listenHost, regressionSMTPBehavior{})
			password := " opaque\npassword "
			err := composuremail.Send(context.Background(), regressionRelay(server, tc.relayHost, password), composuremail.Message{
				To:      "recipient@example.test",
				Subject: "loopback authentication",
				Body:    "body",
			})
			if err != nil {
				t.Fatalf("Send() error = %v", err)
			}
			server.waitClosed(t)
			state := server.state()
			if !state.authenticated || state.password != password {
				t.Errorf("authentication = %t with password %q, want authenticated with opaque password", state.authenticated, state.password)
			}
			if !state.mail || !state.recipient || !state.dataAccepted {
				t.Errorf("SMTP stages = %+v, want AUTH, MAIL, RCPT, and accepted DATA", state)
			}
		})
	}
}

func TestSendAuthenticatedPlaintextReachesRecipientRejectionWithoutLoggingSecrets(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	server := newRegressionSMTPServer(t, "127.0.0.1", regressionSMTPBehavior{rejectRecipient: true})
	password := "opaque-password-secret"
	message := composuremail.Message{To: "recipient-secret@example.test", Subject: "subject-secret", Body: "body-secret"}
	err := composuremail.Send(context.Background(), regressionRelay(server, "127.0.0.1", password), message)
	if err == nil {
		t.Fatal("Send() error = nil, want recipient rejection")
	}
	state := server.state()
	if !state.authenticated || !state.mail || !state.recipient {
		t.Fatalf("SMTP stages = %+v, want authenticated MAIL and rejected RCPT reached", state)
	}
	server.waitClosed(t)
	for _, secret := range []string{server.addr(), "relay-user", password, "sender-secret@example.test", message.To, message.Subject, message.Body} {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("log output exposes sensitive value %q: %q", secret, logs.String())
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error exposes sensitive value %q: %q", secret, err)
		}
	}
}

func TestSendReportsSuccessAfterDataAcceptedWhenShutdownFails(t *testing.T) {
	for _, tc := range []struct {
		name       string
		quitAction string
		cancel     bool
	}{
		{name: "server drops QUIT", quitAction: "drop"},
		{name: "server rejects QUIT", quitAction: "reject"},
		{name: "context canceled during QUIT", quitAction: "hold", cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := newRegressionSMTPServer(t, "127.0.0.1", regressionSMTPBehavior{quitAction: tc.quitAction})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				relay := regressionRelay(server, "127.0.0.1", "opaque-password")
				relay.Username, relay.Password = "", ""
				done <- composuremail.Send(ctx, relay, composuremail.Message{
					To: "recipient@example.test", Subject: "accepted", Body: "body",
				})
			}()
			server.waitDataAccepted(t)
			server.waitQuit(t)
			if tc.cancel {
				cancel()
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("Send() error after DATA acceptance = %v, want nil", err)
				}
			case <-time.After(sendTimeout):
				t.Fatal("Send() did not finish after accepted DATA")
			}
			server.waitClosed(t)
			if !server.state().dataAccepted {
				t.Fatal("server did not accept DATA before shutdown failure")
			}
		})
	}
}

func TestSendRejectsAuthenticatedPlaintextForNonLoopbackBeforeDial(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := composuremail.Send(ctx, config.SMTP{
		Host: "192.0.2.1", Port: 2525, From: "sender@example.test", TLS: "plaintext",
		Username: "relay-user", Password: "opaque-password",
	}, composuremail.Message{To: "recipient@example.test", Subject: "subject", Body: "body"})
	if err == nil {
		t.Fatal("Send() error = nil, want non-loopback plaintext rejection")
	}
	if ctx.Err() != nil {
		t.Fatal("non-loopback plaintext validation attempted a network connection")
	}
}

type regressionSMTPBehavior struct {
	rejectRecipient bool
	quitAction      string
}

type regressionSMTPState struct {
	authenticated bool
	password      string
	mail          bool
	recipient     bool
	dataAccepted  bool
}

type regressionSMTPServer struct {
	t        *testing.T
	listener net.Listener
	behavior regressionSMTPBehavior

	mu           sync.Mutex
	stateValue   regressionSMTPState
	dataAccepted chan struct{}
	quitSeen     chan struct{}
	closed       chan struct{}
	closeOnce    sync.Once
}

func newRegressionSMTPServer(t *testing.T, listenHost string, behavior regressionSMTPBehavior) *regressionSMTPServer {
	t.Helper()
	listener, err := net.Listen("tcp", net.JoinHostPort(listenHost, "0"))
	if err != nil {
		t.Fatalf("listen on loopback %q: %v", listenHost, err)
	}
	server := &regressionSMTPServer{
		t: t, listener: listener, behavior: behavior,
		dataAccepted: make(chan struct{}), quitSeen: make(chan struct{}), closed: make(chan struct{}),
	}
	go server.serve()
	t.Cleanup(server.close)
	return server
}

func (s *regressionSMTPServer) addr() string { return s.listener.Addr().String() }

func (s *regressionSMTPServer) serve() {
	conn, err := s.listener.Accept()
	if err != nil {
		return
	}
	defer close(s.closed)
	defer conn.Close()
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	write := func(response string) bool {
		_, err := io.WriteString(writer, response)
		return err == nil && writer.Flush() == nil
	}
	if !write("220 localhost ESMTP regression\r\n") {
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
			if !write("250-localhost\r\n250-AUTH PLAIN\r\n250 OK\r\n") {
				return
			}
		case strings.HasPrefix(upper, "AUTH PLAIN "):
			encoded := strings.TrimSpace(command[len("AUTH PLAIN "):])
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			parts := bytes.Split(decoded, []byte{0})
			if err != nil || len(parts) != 3 || string(parts[1]) != "relay-user" {
				write("535 authentication failed\r\n")
				return
			}
			s.mu.Lock()
			s.stateValue.authenticated = true
			s.stateValue.password = string(parts[2])
			s.mu.Unlock()
			if !write("235 authenticated\r\n") {
				return
			}
		case strings.HasPrefix(upper, "MAIL FROM:"):
			s.mu.Lock()
			s.stateValue.mail = true
			s.mu.Unlock()
			if !write("250 sender OK\r\n") {
				return
			}
		case strings.HasPrefix(upper, "RCPT TO:"):
			s.mu.Lock()
			s.stateValue.recipient = true
			s.mu.Unlock()
			if s.behavior.rejectRecipient {
				write("550 recipient rejected\r\n")
				return
			}
			if !write("250 recipient OK\r\n") {
				return
			}
		case upper == "DATA":
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
			if !write("250 accepted\r\n") {
				return
			}
			s.mu.Lock()
			s.stateValue.dataAccepted = true
			s.mu.Unlock()
			close(s.dataAccepted)
		case upper == "QUIT":
			close(s.quitSeen)
			switch s.behavior.quitAction {
			case "drop":
				return
			case "reject":
				write("500 shutdown rejected\r\n")
				return
			case "hold":
				_, _ = io.Copy(io.Discard, reader)
				return
			default:
				write("221 bye\r\n")
				return
			}
		default:
			if !write("500 unsupported\r\n") {
				return
			}
		}
	}
}

func regressionRelay(server *regressionSMTPServer, host, password string) config.SMTP {
	_, port, err := net.SplitHostPort(server.addr())
	if err != nil {
		server.t.Fatalf("split server address: %v", err)
	}
	var portNumber int
	if _, err := fmt.Sscanf(port, "%d", &portNumber); err != nil {
		server.t.Fatalf("parse server port: %v", err)
	}
	return config.SMTP{
		Host: host, Port: portNumber, From: "sender-secret@example.test", TLS: "plaintext",
		Username: "relay-user", Password: password,
	}
}

func (s *regressionSMTPServer) state() regressionSMTPState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stateValue
}

func (s *regressionSMTPServer) waitDataAccepted(t *testing.T) {
	t.Helper()
	select {
	case <-s.dataAccepted:
	case <-time.After(sendTimeout):
		t.Fatal("server did not accept DATA")
	}
}

func (s *regressionSMTPServer) waitQuit(t *testing.T) {
	t.Helper()
	select {
	case <-s.quitSeen:
	case <-time.After(sendTimeout):
		t.Fatal("server did not receive QUIT")
	}
}

func (s *regressionSMTPServer) waitClosed(t *testing.T) {
	t.Helper()
	select {
	case <-s.closed:
	case <-time.After(sendTimeout):
		t.Fatal("SMTP connection did not close")
	}
}

func (s *regressionSMTPServer) close() {
	s.closeOnce.Do(func() { _ = s.listener.Close() })
}

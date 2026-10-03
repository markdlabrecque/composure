package mailtest

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testTimeout = 2 * time.Second

func TestServerCapturesStandardLibraryDelivery(t *testing.T) {
	server := New(t)

	host, port, err := net.SplitHostPort(server.Addr())
	if err != nil {
		t.Fatalf("split capture server address: %v", err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		t.Fatalf("capture server host = %q, want loopback address", host)
	}
	if portNumber, err := strconv.Atoi(port); err != nil || portNumber == 0 {
		t.Fatalf("capture server port = %q, want allocated ephemeral port", port)
	}

	from := "sender@example.test"
	to := []string{"first@example.test", "second@example.test"}
	body := []byte("From: sender@example.test\r\n" +
		"To: first@example.test, second@example.test\r\n" +
		"Subject: capture fixture\r\n" +
		"\r\n" +
		"fixture message\r\n")
	if err := smtp.SendMail(server.Addr(), nil, from, to, body); err != nil {
		t.Fatalf("send through capture server: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	got, err := server.WaitForMessage(ctx)
	if err != nil {
		t.Fatalf("wait for captured message: %v", err)
	}
	if got.From != from {
		t.Errorf("envelope sender = %q, want %q", got.From, from)
	}
	if len(got.To) != len(to) || got.To[0] != to[0] || got.To[1] != to[1] {
		t.Errorf("envelope recipients = %q, want %q", got.To, to)
	}
	if !bytes.Equal(got.Data, body) {
		t.Error("captured message data differs from delivered message")
	}
}

func TestMessagesReturnsDeepCopy(t *testing.T) {
	server := New(t)
	body := []byte("Subject: immutable snapshot\r\n\r\noriginal\r\n")
	if err := smtp.SendMail(server.Addr(), nil, "sender@example.test", []string{"recipient@example.test"}, body); err != nil {
		t.Fatalf("send through capture server: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if _, err := server.WaitForMessage(ctx); err != nil {
		t.Fatalf("wait for captured message: %v", err)
	}

	first := server.Messages()
	if len(first) != 1 {
		t.Fatalf("captured message count = %d, want 1", len(first))
	}
	first[0].From = "changed@example.test"
	first[0].To[0] = "changed@example.test"
	first[0].Data[0] = 'X'
	first = append(first, Message{})

	second := server.Messages()
	if len(second) != 1 {
		t.Fatalf("captured message count after snapshot mutation = %d, want 1", len(second))
	}
	if second[0].From != "sender@example.test" {
		t.Error("snapshot mutation changed stored envelope sender")
	}
	if len(second[0].To) != 1 || second[0].To[0] != "recipient@example.test" {
		t.Error("snapshot mutation changed stored envelope recipients")
	}
	if !bytes.Equal(second[0].Data, body) {
		t.Error("snapshot mutation changed stored message data")
	}
}

func TestWaitForMessageReturnsContextError(t *testing.T) {
	server := New(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := server.WaitForMessage(ctx); err != context.Canceled {
		t.Fatalf("WaitForMessage error = %v, want context.Canceled", err)
	}
}

func TestCleanupClosesClientsBlockedMidTransaction(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, net.Conn)
	}{
		{
			name: "mid-command",
			prepare: func(t *testing.T, conn net.Conn) {
				readSMTPResponse(t, conn, 220)
				if _, err := fmt.Fprint(conn, "MAI"); err != nil {
					t.Fatalf("write partial SMTP command: %v", err)
				}
			},
		},
		{
			name: "during-data",
			prepare: func(t *testing.T, conn net.Conn) {
				readSMTPResponse(t, conn, 220)
				writeSMTPCommand(t, conn, "EHLO example.test\r\n", 250)
				writeSMTPCommand(t, conn, "MAIL FROM:<sender@example.test>\r\n", 250)
				writeSMTPCommand(t, conn, "RCPT TO:<recipient@example.test>\r\n", 250)
				writeSMTPCommand(t, conn, "DATA\r\n", 354)
				if _, err := fmt.Fprint(conn, "Subject: unfinished\r\n\r\npartial body"); err != nil {
					t.Fatalf("write partial SMTP data: %v", err)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var conn net.Conn
			if ok := t.Run("fixture", func(t *testing.T) {
				server := New(t)
				var err error
				conn, err = net.DialTimeout("tcp", server.Addr(), testTimeout)
				if err != nil {
					t.Fatalf("dial capture server: %v", err)
				}
				tc.prepare(t, conn)
			}); !ok {
				t.Fatal("fixture setup failed")
			}
			defer conn.Close()

			if err := conn.SetReadDeadline(time.Now().Add(testTimeout)); err != nil {
				t.Fatalf("set cleanup observation deadline: %v", err)
			}
			if _, err := bufio.NewReader(conn).ReadByte(); err == nil {
				t.Fatal("client connection remained open after fixture cleanup")
			} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				t.Fatal("fixture cleanup did not close client connection before deadline")
			}
		})
	}
}

func TestCloseStopsListenerAndIsIdempotent(t *testing.T) {
	server := New(t)
	addr := server.Addr()
	server.Close()
	server.Close()

	conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
	if err == nil {
		conn.Close()
		t.Fatal("capture server accepted a connection after Close")
	}
}

func writeSMTPCommand(t *testing.T, conn net.Conn, command string, wantCode int) {
	t.Helper()
	if _, err := fmt.Fprint(conn, command); err != nil {
		t.Fatalf("write SMTP command: %v", err)
	}
	readSMTPResponse(t, conn, wantCode)
}

func readSMTPResponse(t *testing.T, conn net.Conn, wantCode int) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(testTimeout)); err != nil {
		t.Fatalf("set SMTP response deadline: %v", err)
	}
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SMTP response: %v", err)
		}
		if len(line) < 4 {
			t.Fatalf("short SMTP response: %q", strings.TrimSpace(line))
		}
		code, err := strconv.Atoi(line[:3])
		if err != nil {
			t.Fatalf("invalid SMTP response code: %q", line[:3])
		}
		if code != wantCode {
			t.Fatalf("SMTP response code = %d, want %d", code, wantCode)
		}
		if line[3] == ' ' {
			return
		}
		if line[3] != '-' {
			t.Fatalf("invalid SMTP response separator in %q", strings.TrimSpace(line))
		}
	}
}

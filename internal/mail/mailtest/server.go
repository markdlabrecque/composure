package mailtest

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
)

// Message is a captured SMTP envelope and message body.
type Message struct {
	From string
	To   []string
	Data []byte
}

// Server is a loopback-only SMTP server intended for tests.
type Server struct {
	listener net.Listener

	mu       sync.Mutex
	messages []Message
	clients  map[net.Conn]struct{}
	changed  chan struct{}

	closeOnce  sync.Once
	acceptDone chan struct{}
	handlers   sync.WaitGroup
}

// New starts an SMTP capture server and registers cleanup with t.
func New(t testing.TB) *Server {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for SMTP capture server: %v", err)
	}
	server := &Server{
		listener:   listener,
		clients:    make(map[net.Conn]struct{}),
		changed:    make(chan struct{}),
		acceptDone: make(chan struct{}),
	}
	go server.accept()
	t.Cleanup(server.Close)
	return server
}

// Addr returns the server's loopback listener address.
func (s *Server) Addr() string { return s.listener.Addr().String() }

// WaitForMessage waits for the first captured message or for ctx to finish.
func (s *Server) WaitForMessage(ctx context.Context) (Message, error) {
	for {
		s.mu.Lock()
		if len(s.messages) != 0 {
			message := cloneMessage(s.messages[0])
			s.mu.Unlock()
			return message, nil
		}
		changed := s.changed
		s.mu.Unlock()

		select {
		case <-ctx.Done():
			return Message{}, ctx.Err()
		case <-changed:
		}
	}
}

// Messages returns a deep copy of all captured messages.
func (s *Server) Messages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	messages := make([]Message, len(s.messages))
	for i, message := range s.messages {
		messages[i] = cloneMessage(message)
	}
	return messages
}

// Close stops accepting connections, closes active clients, and joins handlers.
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		_ = s.listener.Close()
		<-s.acceptDone
		s.mu.Lock()
		for conn := range s.clients {
			_ = conn.Close()
		}
		s.mu.Unlock()
		s.handlers.Wait()
	})
}

func (s *Server) accept() {
	defer close(s.acceptDone)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.clients[conn] = struct{}{}
		s.handlers.Add(1)
		s.mu.Unlock()
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer s.handlers.Done()
	defer func() {
		_ = conn.Close()
		s.mu.Lock()
		delete(s.clients, conn)
		s.mu.Unlock()
	}()
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	write := func(response string) bool {
		if _, err := io.WriteString(writer, response); err != nil {
			return false
		}
		return writer.Flush() == nil
	}
	if !write("220 localhost ESMTP mailtest\r\n") {
		return
	}
	from := ""
	var recipients []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(command)
		switch {
		case strings.HasPrefix(upper, "EHLO "):
			if !write("250-localhost\r\n250 OK\r\n") {
				return
			}
		case strings.HasPrefix(upper, "HELO "):
			if !write("250 localhost\r\n") {
				return
			}
		case strings.HasPrefix(upper, "MAIL FROM:"):
			from = addressArgument(command[len("MAIL FROM:"):])
			recipients = nil
			if !write("250 sender OK\r\n") {
				return
			}
		case strings.HasPrefix(upper, "RCPT TO:"):
			recipients = append(recipients, addressArgument(command[len("RCPT TO:"):]))
			if !write("250 recipient OK\r\n") {
				return
			}
		case upper == "DATA":
			if !write("354 end with <CRLF>.<CRLF>\r\n") {
				return
			}
			data, err := readMessageData(reader)
			if err != nil {
				return
			}
			s.mu.Lock()
			s.messages = append(s.messages, Message{From: from, To: append([]string(nil), recipients...), Data: data})
			close(s.changed)
			s.changed = make(chan struct{})
			s.mu.Unlock()
			if !write("250 message accepted\r\n") {
				return
			}
			from, recipients = "", nil
		case upper == "RSET":
			from, recipients = "", nil
			if !write("250 reset\r\n") {
				return
			}
		case upper == "NOOP":
			if !write("250 OK\r\n") {
				return
			}
		case upper == "QUIT":
			write("221 bye\r\n")
			return
		default:
			if !write("500 unsupported command\r\n") {
				return
			}
		}
	}
}

func addressArgument(value string) string {
	value = strings.TrimSpace(value)
	if left := strings.IndexByte(value, '<'); left >= 0 {
		if right := strings.IndexByte(value[left+1:], '>'); right >= 0 {
			return value[left+1 : left+1+right]
		}
	}
	return strings.Trim(value, "<>")
}

func readMessageData(reader *bufio.Reader) ([]byte, error) {
	var data []byte
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		if string(line) == ".\r\n" || string(line) == ".\n" {
			return data, nil
		}
		if bytesHasDotPrefix(line) {
			line = line[1:]
		}
		data = append(data, line...)
	}
}

func bytesHasDotPrefix(line []byte) bool { return len(line) >= 2 && line[0] == '.' && line[1] == '.' }

func cloneMessage(message Message) Message {
	return Message{From: message.From, To: append([]string(nil), message.To...), Data: append([]byte(nil), message.Data...)}
}

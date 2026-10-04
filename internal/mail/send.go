// Package mail sends plain-text messages through a configured SMTP relay.
package mail

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/markdlabrecque/composure/internal/config"
)

const sendTimeout = 30 * time.Second

var (
	errInvalidRelay   = errors.New("invalid SMTP relay configuration")
	errInvalidMessage = errors.New("invalid SMTP message")
	errConnect        = errors.New("SMTP connection failed")
	errProtocol       = errors.New("SMTP protocol failed")
	errTLS            = errors.New("SMTP TLS negotiation failed")
	errAuth           = errors.New("SMTP authentication failed")
	errEnvelope       = errors.New("SMTP envelope was rejected")
	errDelivery       = errors.New("SMTP message delivery failed")
)

// Message is a single plain-text email. To is the sole envelope recipient.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Option configures a Send operation.
type Option func(*sendOptions)

type sendOptions struct {
	roots *x509.CertPool
}

// WithRootCAs supplies additional trust roots for TLS verification. Normal
// certificate chain and hostname checks remain enabled.
func WithRootCAs(roots *x509.CertPool) Option {
	return func(options *sendOptions) { options.roots = roots }
}

// Send delivers one plain-text message. The relay is validated before any
// network access, and all transport work is bounded by the context deadline
// or a 30-second default timeout. Errors use fixed categories and never
// include relay, credential, recipient, or message values.
func Send(ctx context.Context, relay config.SMTP, message Message, options ...Option) error {
	if ctx == nil {
		return errInvalidMessage
	}
	if err := validateRelay(relay); err != nil {
		return err
	}
	if err := validateMessage(message); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	settings := sendOptions{}
	for _, option := range options {
		if option != nil {
			option(&settings)
		}
	}

	deadline := time.Now().Add(sendTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	dialer := net.Dialer{Deadline: deadline}
	address := net.JoinHostPort(relay.Host, strconv.Itoa(relay.Port))
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return contextFailure(ctx, errConnect)
	}
	_ = conn.SetDeadline(deadline)
	stopCancelClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer func() {
		stopCancelClose()
		_ = conn.Close()
	}()

	mode := relay.TLS
	if mode == "" {
		mode = "starttls"
	}
	secure := mode != "plaintext"
	if mode == "implicit" {
		conn = tls.Client(conn, tlsConfig(relay.Host, settings.roots))
		if err := conn.(*tls.Conn).HandshakeContext(ctx); err != nil {
			return contextFailure(ctx, errTLS)
		}
	}

	client, err := smtp.NewClient(conn, relay.Host)
	if err != nil {
		return contextFailure(ctx, errProtocol)
	}
	clientOpen := true
	defer func() {
		if clientOpen {
			_ = client.Close()
		}
	}()
	if err := client.Hello("localhost"); err != nil {
		return contextFailure(ctx, errProtocol)
	}
	if mode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errTLS
		}
		if err := client.StartTLS(tlsConfig(relay.Host, settings.roots)); err != nil {
			return contextFailure(ctx, errTLS)
		}
	}
	if relay.Username != "" {
		var auth smtp.Auth = smtp.PlainAuth("", relay.Username, relay.Password, relay.Host)
		if !secure {
			auth = loopbackPlainAuth{
				username: relay.Username,
				password: relay.Password,
				host:     relay.Host,
			}
		}
		if err := client.Auth(auth); err != nil {
			return contextFailure(ctx, errAuth)
		}
	}
	if err := client.Mail(relay.From); err != nil {
		return contextFailure(ctx, errEnvelope)
	}
	if err := client.Rcpt(message.To); err != nil {
		return contextFailure(ctx, errEnvelope)
	}
	writer, err := client.Data()
	if err != nil {
		return contextFailure(ctx, errDelivery)
	}
	if _, err := writer.Write(serialize(relay.From, message)); err != nil {
		_ = writer.Close()
		return contextFailure(ctx, errDelivery)
	}
	if err := writer.Close(); err != nil {
		return contextFailure(ctx, errDelivery)
	}
	// A successful DATA close means the relay accepted the message. QUIT is
	// bounded best-effort cleanup; its failure cannot turn delivery into a
	// reported failure that a caller might retry as a duplicate.
	_ = client.Quit()
	clientOpen = false
	return nil
}

type loopbackPlainAuth struct {
	username string
	password string
	host     string
}

func (a loopbackPlainAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if server.TLS || server.Name != a.host || !localHost(a.host) {
		return "", nil, errors.New("unencrypted connection")
	}
	return "PLAIN", []byte("\x00" + a.username + "\x00" + a.password), nil
}

func (loopbackPlainAuth) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("unexpected SMTP authentication challenge")
	}
	return nil, nil
}

func contextFailure(ctx context.Context, fallback error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fallback
}

func validateRelay(relay config.SMTP) error {
	if relay.Host == "" || relay.Port < 1 || relay.Port > 65535 || relay.From == "" {
		return errInvalidRelay
	}
	if !validHost(relay.Host) || !validMailbox(relay.From) {
		return errInvalidRelay
	}
	if hasControl(relay.Username) || (relay.Username == "") != (relay.Password == "") {
		return errInvalidRelay
	}
	mode := relay.TLS
	if mode == "" {
		mode = "starttls"
	}
	switch mode {
	case "starttls", "implicit":
	case "plaintext":
		if !localHost(relay.Host) {
			return errInvalidRelay
		}
	default:
		return errInvalidRelay
	}
	return nil
}

func validateMessage(message Message) error {
	if !validMailbox(message.To) || message.Subject == "" || hasControl(message.Subject) || !utf8.ValidString(message.Subject) || !utf8.ValidString(message.Body) {
		return errInvalidMessage
	}
	return nil
}

func validMailbox(value string) bool {
	if value == "" || hasControl(value) {
		return false
	}
	parsed, err := mail.ParseAddress(value)
	return err == nil && parsed.Name == "" && parsed.Address == value
}

func validHost(host string) bool {
	if host == "" || strings.ContainsAny(host, "\r\n") || hasControl(host) {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	name := strings.TrimSuffix(host, ".")
	if len(name) == 0 || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}

func localHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func hasControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func tlsConfig(host string, roots *x509.CertPool) *tls.Config {
	return &tls.Config{ServerName: host, RootCAs: roots, MinVersion: tls.VersionTLS12}
}

func serialize(from string, message Message) []byte {
	var output strings.Builder
	output.WriteString("From: ")
	output.WriteString(from)
	output.WriteString("\r\nTo: ")
	output.WriteString(message.To)
	output.WriteString("\r\nSubject: ")
	output.WriteString(message.Subject)
	output.WriteString("\r\nContent-Type: text/plain; charset=UTF-8\r\nMIME-Version: 1.0\r\n\r\n")
	body := strings.ReplaceAll(message.Body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	output.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	if !strings.HasSuffix(body, "\n") {
		output.WriteString("\r\n")
	}
	return []byte(output.String())
}

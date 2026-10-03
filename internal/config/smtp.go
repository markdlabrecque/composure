package config

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/mail"
	"os"
	"strconv"
	"strings"
)

// SMTP contains relay settings read from COMPOSURE_SMTP_* environment
// variables. A zero value means SMTP is disabled. When configured without an
// explicit COMPOSURE_SMTP_TLS value, TLS defaults to STARTTLS.
//
// Password is a deployment secret and must not be serialized, logged, or
// included in audit events.
type SMTP struct {
	Host     string `json:"-"`
	Port     int    `json:"-"`
	From     string `json:"-"`
	Username string `json:"-"`
	Password string `json:"-"`
	TLS      string `json:"-"`
}

// String returns a safe description of SMTP without exposing deployment values.
func (SMTP) String() string { return "SMTP{redacted}" }

// GoString returns a safe Go-syntax description of SMTP without exposing deployment values.
func (SMTP) GoString() string { return "SMTP{redacted}" }

// Format keeps fmt's ordinary and debug formatting from exposing deployment values.
func (SMTP) Format(state fmt.State, verb rune) {
	_, _ = io.WriteString(state, "SMTP{redacted}")
}

// LogValue keeps structured slog output from exposing deployment values.
func (SMTP) LogValue() slog.Value { return slog.StringValue("SMTP{redacted}") }

// LoadSMTP reads and validates COMPOSURE_SMTP_HOST, PORT, FROM, USERNAME,
// PASSWORD, and TLS from the process environment. All six unset or empty
// values disable SMTP; partial configuration is an error. Username and
// password must be supplied together. TLS accepts starttls, implicit, or
// plaintext; plaintext is allowed only for localhost and loopback IPs.
// Validation is syntactic and performs no DNS lookup or network connection.
// Non-secret values are trimmed, while the password is preserved verbatim.
func LoadSMTP() (SMTP, error) {
	read := func(key string) string { return os.Getenv("COMPOSURE_SMTP_" + key) }
	raw := map[string]string{
		"HOST": read("HOST"), "PORT": read("PORT"), "FROM": read("FROM"),
		"USERNAME": read("USERNAME"), "PASSWORD": read("PASSWORD"), "TLS": read("TLS"),
	}
	allEmpty := true
	for _, value := range raw {
		if value != "" {
			allEmpty = false
			break
		}
	}
	if allEmpty {
		return SMTP{}, nil
	}

	trimmed := make(map[string]string, len(raw))
	for key, value := range raw {
		if key != "PASSWORD" && hasControl(value) {
			return SMTP{}, fmt.Errorf("invalid SMTP configuration")
		}
		trimmed[key] = strings.TrimSpace(value)
	}
	host, portText, from := trimmed["HOST"], trimmed["PORT"], trimmed["FROM"]
	if host == "" || portText == "" || from == "" {
		return SMTP{}, fmt.Errorf("incomplete SMTP configuration")
	}
	if !validSMTPHost(host) {
		return SMTP{}, fmt.Errorf("invalid SMTP host")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return SMTP{}, fmt.Errorf("invalid SMTP port")
	}
	if strings.ContainsAny(from, "\r\n") || hasControl(from) {
		return SMTP{}, fmt.Errorf("invalid SMTP sender")
	}
	address, err := mail.ParseAddress(from)
	if err != nil || address.Name != "" || address.Address != from {
		return SMTP{}, fmt.Errorf("invalid SMTP sender")
	}
	username, password := trimmed["USERNAME"], raw["PASSWORD"]
	if hasControl(username) {
		return SMTP{}, fmt.Errorf("invalid SMTP username")
	}
	if (username == "") != (password == "") {
		return SMTP{}, fmt.Errorf("SMTP username and password must be supplied together")
	}
	tlsMode := trimmed["TLS"]
	if tlsMode == "" {
		tlsMode = "starttls"
	}
	switch tlsMode {
	case "starttls", "implicit":
	case "plaintext":
		if !isLocalSMTPHost(host) {
			return SMTP{}, fmt.Errorf("plaintext SMTP is allowed only for localhost or loopback")
		}
	default:
		return SMTP{}, fmt.Errorf("invalid SMTP TLS mode")
	}
	return SMTP{Host: host, Port: port, From: from, Username: username, Password: password, TLS: tlsMode}, nil
}

func validSMTPHost(host string) bool {
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
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' {
				return false
			}
		}
	}
	return true
}

func isLocalSMTPHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func hasControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

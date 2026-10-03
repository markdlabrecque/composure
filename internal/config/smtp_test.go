package config

import (
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

var smtpEnvironment = []string{
	"COMPOSURE_SMTP_HOST",
	"COMPOSURE_SMTP_PORT",
	"COMPOSURE_SMTP_FROM",
	"COMPOSURE_SMTP_USERNAME",
	"COMPOSURE_SMTP_PASSWORD",
	"COMPOSURE_SMTP_TLS",
}

func setSMTPEnvironment(t *testing.T, values map[string]string) {
	t.Helper()
	for _, name := range smtpEnvironment {
		t.Setenv(name, values[name])
	}
}

func unsetSMTPEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range smtpEnvironment {
		value, present := os.LookupEnv(name)
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
		t.Cleanup(func() {
			if present {
				_ = os.Setenv(name, value)
			} else {
				_ = os.Unsetenv(name)
			}
		})
	}
}

func TestLoadSMTPAbsentReturnsZeroConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*testing.T)
	}{
		{name: "unset", set: unsetSMTPEnvironment},
		{name: "empty", set: func(t *testing.T) { setSMTPEnvironment(t, nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.set(t)
			got, err := LoadSMTP()
			if err != nil {
				t.Fatal("LoadSMTP rejected absent SMTP environment")
			}
			if !reflect.DeepEqual(got, SMTP{}) {
				t.Errorf("LoadSMTP with absent SMTP environment = %#v, want zero SMTP configuration", got)
			}
		})
	}
}

func TestLoadSMTPConfiguredAndSecureDefault(t *testing.T) {
	setSMTPEnvironment(t, map[string]string{
		"COMPOSURE_SMTP_HOST":     "  smtp.example.test  ",
		"COMPOSURE_SMTP_PORT":     " 2525 ",
		"COMPOSURE_SMTP_FROM":     " sender@example.test ",
		"COMPOSURE_SMTP_USERNAME": " relay-user ",
		"COMPOSURE_SMTP_PASSWORD": "  password kept verbatim\t",
	})

	got, err := LoadSMTP()
	if err != nil {
		t.Fatal("LoadSMTP rejected valid SMTP environment")
	}
	if got.Host != "smtp.example.test" || got.Port != 2525 || got.From != "sender@example.test" || got.Username != "relay-user" || got.TLS != "starttls" {
		t.Errorf("LoadSMTP non-secret fields = host %q, port %d, from %q, username %q, TLS %q", got.Host, got.Port, got.From, got.Username, got.TLS)
	}
	if got.Password != "  password kept verbatim\t" {
		t.Error("LoadSMTP did not preserve the password verbatim")
	}
}

func TestLoadSMTPTLSModes(t *testing.T) {
	for _, tc := range []struct {
		name, host, mode string
	}{
		{name: "starttls", host: "smtp.example.test", mode: "starttls"},
		{name: "implicit", host: "smtp.example.test", mode: "implicit"},
		{name: "plaintext_localhost", host: "localhost", mode: "plaintext"},
		{name: "plaintext_ipv4_loopback", host: "127.0.0.1", mode: "plaintext"},
		{name: "plaintext_ipv6_loopback", host: "::1", mode: "plaintext"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setSMTPEnvironment(t, map[string]string{
				"COMPOSURE_SMTP_HOST": tc.host,
				"COMPOSURE_SMTP_PORT": "1025",
				"COMPOSURE_SMTP_FROM": "sender@example.test",
				"COMPOSURE_SMTP_TLS":  " " + tc.mode + " ",
			})
			got, err := LoadSMTP()
			if err != nil {
				t.Fatalf("LoadSMTP rejected valid %s configuration", tc.name)
			}
			if got.TLS != tc.mode {
				t.Errorf("TLS = %q, want %q", got.TLS, tc.mode)
			}
		})
	}
}

func TestLoadSMTPRejectsPartialAndInvalidConfiguration(t *testing.T) {
	valid := map[string]string{
		"COMPOSURE_SMTP_HOST": "smtp.example.test",
		"COMPOSURE_SMTP_PORT": "587",
		"COMPOSURE_SMTP_FROM": "sender@example.test",
		"COMPOSURE_SMTP_TLS":  "starttls",
	}
	cases := []struct {
		name   string
		change map[string]string
	}{
		{name: "host_only", change: map[string]string{"COMPOSURE_SMTP_PORT": "", "COMPOSURE_SMTP_FROM": ""}},
		{name: "missing_host", change: map[string]string{"COMPOSURE_SMTP_HOST": ""}},
		{name: "missing_port", change: map[string]string{"COMPOSURE_SMTP_PORT": ""}},
		{name: "missing_sender", change: map[string]string{"COMPOSURE_SMTP_FROM": ""}},
		{name: "username_without_password", change: map[string]string{"COMPOSURE_SMTP_USERNAME": "relay-user"}},
		{name: "password_without_username", change: map[string]string{"COMPOSURE_SMTP_PASSWORD": "relay-password"}},
		{name: "invalid_tls", change: map[string]string{"COMPOSURE_SMTP_TLS": "opportunistic"}},
		{name: "remote_plaintext", change: map[string]string{"COMPOSURE_SMTP_TLS": "plaintext"}},
		{name: "port_text", change: map[string]string{"COMPOSURE_SMTP_PORT": "submission"}},
		{name: "port_zero", change: map[string]string{"COMPOSURE_SMTP_PORT": "0"}},
		{name: "port_too_large", change: map[string]string{"COMPOSURE_SMTP_PORT": "65536"}},
		{name: "host_url", change: map[string]string{"COMPOSURE_SMTP_HOST": "https://smtp.example.test"}},
		{name: "host_with_port", change: map[string]string{"COMPOSURE_SMTP_HOST": "smtp.example.test:587"}},
		{name: "host_with_path", change: map[string]string{"COMPOSURE_SMTP_HOST": "smtp.example.test/relay"}},
		{name: "host_bad_label", change: map[string]string{"COMPOSURE_SMTP_HOST": "-smtp.example.test"}},
		{name: "sender_not_mailbox", change: map[string]string{"COMPOSURE_SMTP_FROM": "not-an-address"}},
		{name: "sender_header_injection", change: map[string]string{"COMPOSURE_SMTP_FROM": "sender@example.test\r\nBcc: victim@example.test"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := make(map[string]string, len(valid))
			for name, value := range valid {
				values[name] = value
			}
			for name, value := range tc.change {
				values[name] = value
			}
			setSMTPEnvironment(t, values)
			if _, err := LoadSMTP(); err == nil {
				t.Error("LoadSMTP accepted invalid SMTP environment")
			}
		})
	}
}

func TestLoadSMTPDiagnosticsDoNotExposeEnvironmentValues(t *testing.T) {
	values := map[string]string{
		"COMPOSURE_SMTP_HOST":     "smtp-secret-host.invalid:587",
		"COMPOSURE_SMTP_PORT":     strconv.Itoa(70000),
		"COMPOSURE_SMTP_FROM":     "private-sender@example.invalid",
		"COMPOSURE_SMTP_USERNAME": "private-relay-user",
		"COMPOSURE_SMTP_PASSWORD": "private-relay-password",
		"COMPOSURE_SMTP_TLS":      "private-transport-mode",
	}
	setSMTPEnvironment(t, values)

	_, err := LoadSMTP()
	if err == nil {
		t.Fatal("LoadSMTP accepted invalid SMTP environment")
	}
	diagnostic := err.Error()
	for name, value := range values {
		if strings.Contains(diagnostic, value) {
			t.Errorf("diagnostic exposes the value of %s", name)
		}
	}
}

package config

import (
	"strings"
	"testing"
)

func TestLoadSMTPRejectsRawUnicodeControlsBeforeTrimming(t *testing.T) {
	valid := map[string]string{
		"COMPOSURE_SMTP_HOST":     "smtp.example.test",
		"COMPOSURE_SMTP_PORT":     "587",
		"COMPOSURE_SMTP_FROM":     "sender@example.test",
		"COMPOSURE_SMTP_USERNAME": "relay-user",
		"COMPOSURE_SMTP_PASSWORD": "paired-password",
		"COMPOSURE_SMTP_TLS":      "starttls",
	}
	cases := []struct {
		name, key, value string
	}{
		{name: "host_leading_next_line", key: "COMPOSURE_SMTP_HOST", value: "\u0085smtp.example.test"},
		{name: "host_trailing_next_line", key: "COMPOSURE_SMTP_HOST", value: "smtp.example.test\u0085"},
		{name: "port_leading_next_line", key: "COMPOSURE_SMTP_PORT", value: "\u0085587"},
		{name: "port_trailing_next_line", key: "COMPOSURE_SMTP_PORT", value: "587\u0085"},
		{name: "from_leading_next_line", key: "COMPOSURE_SMTP_FROM", value: "\u0085sender@example.test"},
		{name: "from_trailing_next_line", key: "COMPOSURE_SMTP_FROM", value: "sender@example.test\u0085"},
		{name: "username_leading_next_line", key: "COMPOSURE_SMTP_USERNAME", value: "\u0085relay-user"},
		{name: "username_trailing_next_line", key: "COMPOSURE_SMTP_USERNAME", value: "relay-user\u0085"},
		{name: "tls_leading_next_line", key: "COMPOSURE_SMTP_TLS", value: "\u0085starttls"},
		{name: "tls_trailing_next_line", key: "COMPOSURE_SMTP_TLS", value: "starttls\u0085"},
		{name: "host_c1_padding_character", key: "COMPOSURE_SMTP_HOST", value: "smtp\u0080.example.test"},
		{name: "port_c1_application_program_command", key: "COMPOSURE_SMTP_PORT", value: "58\u009f7"},
		{name: "from_c1_start_of_string", key: "COMPOSURE_SMTP_FROM", value: "sender\u0098@example.test"},
		{name: "username_c1_no_break_here", key: "COMPOSURE_SMTP_USERNAME", value: "relay\u0083-user"},
		{name: "tls_c1_string_terminator", key: "COMPOSURE_SMTP_TLS", value: "start\u009ctls"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := make(map[string]string, len(valid))
			for key, value := range valid {
				values[key] = value
			}
			values[tc.key] = tc.value
			setSMTPEnvironment(t, values)

			_, err := LoadSMTP()
			if err == nil {
				t.Fatal("LoadSMTP accepted Unicode control")
			}
			if strings.Contains(err.Error(), tc.value) {
				t.Fatal("diagnostic exposes rejected input")
			}
		})
	}
}

func TestLoadSMTPPreservesUnicodeControlsInOpaquePassword(t *testing.T) {
	password := "\u0085opaque\u0080password\u009f"
	setSMTPEnvironment(t, map[string]string{
		"COMPOSURE_SMTP_HOST":     "smtp.example.test",
		"COMPOSURE_SMTP_PORT":     "587",
		"COMPOSURE_SMTP_FROM":     "sender@example.test",
		"COMPOSURE_SMTP_USERNAME": "relay-user",
		"COMPOSURE_SMTP_PASSWORD": password,
		"COMPOSURE_SMTP_TLS":      "starttls",
	})

	got, err := LoadSMTP()
	if err != nil {
		t.Fatal("LoadSMTP rejected opaque password controls")
	}
	if got.Password != password {
		t.Fatal("LoadSMTP changed opaque password controls")
	}
}

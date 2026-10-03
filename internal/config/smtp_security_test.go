package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestLoadSMTPRejectsRawControlsBeforeTrimming(t *testing.T) {
	valid := map[string]string{
		"COMPOSURE_SMTP_HOST":     "smtp.example.test",
		"COMPOSURE_SMTP_PORT":     "587",
		"COMPOSURE_SMTP_FROM":     "sender@example.test",
		"COMPOSURE_SMTP_USERNAME": "relay-user",
		"COMPOSURE_SMTP_PASSWORD": "paired-password",
		"COMPOSURE_SMTP_TLS":      "starttls",
	}
	fields := []struct {
		name, key, value string
	}{
		{name: "host", key: "COMPOSURE_SMTP_HOST", value: "smtp.example.test"},
		{name: "port", key: "COMPOSURE_SMTP_PORT", value: "587"},
		{name: "from", key: "COMPOSURE_SMTP_FROM", value: "sender@example.test"},
		{name: "username", key: "COMPOSURE_SMTP_USERNAME", value: "relay-user"},
		{name: "tls", key: "COMPOSURE_SMTP_TLS", value: "starttls"},
	}
	controls := []struct {
		name, value string
	}{
		{name: "leading_cr", value: "\r%s"},
		{name: "trailing_cr", value: "%s\r"},
		{name: "embedded_cr", value: "%s\rblocked"},
		{name: "leading_lf", value: "\n%s"},
		{name: "trailing_lf", value: "%s\n"},
		{name: "embedded_lf", value: "%s\nblocked"},
		{name: "leading_vertical_tab", value: "\v%s"},
		{name: "embedded_tab", value: "%s\tblocked"},
		{name: "trailing_delete", value: "%s\x7f"},
	}

	for _, field := range fields {
		for _, control := range controls {
			t.Run(field.name+"_"+control.name, func(t *testing.T) {
				values := make(map[string]string, len(valid))
				for key, value := range valid {
					values[key] = value
				}
				raw := fmt.Sprintf(control.value, field.value)
				values[field.key] = raw
				setSMTPEnvironment(t, values)

				_, err := LoadSMTP()
				if err == nil {
					t.Fatal("LoadSMTP accepted prohibited control")
				}
				if strings.Contains(err.Error(), raw) {
					t.Fatal("diagnostic exposes rejected input")
				}
			})
		}
	}
}

func TestLoadSMTPStillTrimsNonSecretsAndPreservesPassword(t *testing.T) {
	setSMTPEnvironment(t, map[string]string{
		"COMPOSURE_SMTP_HOST":     "  smtp.example.test  ",
		"COMPOSURE_SMTP_PORT":     "  2525  ",
		"COMPOSURE_SMTP_FROM":     "  sender@example.test  ",
		"COMPOSURE_SMTP_USERNAME": "  relay-user  ",
		"COMPOSURE_SMTP_PASSWORD": "  password-verbatim\t",
		"COMPOSURE_SMTP_TLS":      "  implicit  ",
	})

	got, err := LoadSMTP()
	if err != nil {
		t.Fatal("LoadSMTP rejected ordinary non-secret whitespace")
	}
	if got.Host != "smtp.example.test" || got.Port != 2525 || got.From != "sender@example.test" || got.Username != "relay-user" || got.TLS != "implicit" {
		t.Fatal("LoadSMTP did not trim ordinary non-secret whitespace")
	}
	if got.Password != "  password-verbatim\t" {
		t.Fatal("LoadSMTP did not preserve password verbatim")
	}
}

func TestSMTPDeploymentValuesAreAbsentFromJSON(t *testing.T) {
	settings := SMTP{
		Host:     "json-host-canary.invalid",
		Port:     24680,
		From:     "json-from-canary@example.invalid",
		Username: "json-user-canary",
		Password: "json-password-canary",
		TLS:      "json-tls-canary",
	}

	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatal("SMTP JSON serialization failed")
	}
	if string(encoded) != "{}" {
		t.Fatal("SMTP JSON contains deployment values")
	}
	if settings.Host == "" || settings.Port == 0 || settings.From == "" || settings.Username == "" || settings.Password == "" || settings.TLS == "" {
		t.Fatal("SMTP runtime fields are not usable")
	}
}

func TestSMTPFormattingAndStructuredLoggingRedactPassword(t *testing.T) {
	settings := SMTP{
		Host:     "format-host-canary.invalid",
		Port:     13579,
		From:     "format-from-canary@example.invalid",
		Username: "format-user-canary",
		Password: "format-password-canary",
		TLS:      "format-tls-canary",
	}

	for _, tc := range []struct {
		name, formatted string
	}{
		{name: "default", formatted: fmt.Sprintf("%v", settings)},
		{name: "fields", formatted: fmt.Sprintf("%+v", settings)},
		{name: "go_syntax", formatted: fmt.Sprintf("%#v", settings)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Contains(tc.formatted, settings.Password) {
				t.Fatal("formatted SMTP exposes password")
			}
		})
	}

	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	logger.Info("smtp", slog.Any("settings", settings))
	if strings.Contains(output.String(), settings.Password) {
		t.Fatal("structured SMTP log exposes password")
	}
}

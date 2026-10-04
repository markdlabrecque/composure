package web

import (
	"context"
	"net/url"
	"os"
	"strings"
	"unicode"

	"github.com/markdlabrecque/composure/internal/config"
	"github.com/markdlabrecque/composure/internal/mail"
)

type invitationDelivery struct {
	publicURL *url.URL
	relay     config.SMTP
	send      func(context.Context, config.SMTP, mail.Message) error
	enabled   bool
	err       error
}

type invitationDeliverySource interface {
	InvitationDelivery() (string, config.SMTP, func(context.Context, config.SMTP, mail.Message) error, bool)
}

func invitationDeliveryFromRepository(repository any) invitationDelivery {
	source, ok := repository.(invitationDeliverySource)
	if !ok {
		return invitationDelivery{}
	}
	publicURL, relay, send, enabled := source.InvitationDelivery()
	if !enabled {
		return invitationDelivery{}
	}
	parsed, err := parseInvitationPublicURL(publicURL)
	return invitationDelivery{publicURL: parsed, relay: relay, send: send, enabled: true, err: err}
}

func configuredInvitationDelivery() invitationDelivery {
	relay, err := config.LoadSMTP()
	if err != nil {
		return invitationDelivery{err: err}
	}
	if relay == (config.SMTP{}) {
		return invitationDelivery{}
	}
	raw, present := os.LookupEnv("COMPOSURE_PUBLIC_URL")
	if !present || raw == "" {
		return invitationDelivery{err: errInvalidInvitationPublicURL}
	}
	publicURL, err := parseInvitationPublicURL(raw)
	return invitationDelivery{
		publicURL: publicURL,
		relay:     relay,
		send: func(ctx context.Context, relay config.SMTP, message mail.Message) error {
			return mail.Send(ctx, relay, message)
		},
		enabled: true,
		err:     err,
	}
}

var errInvalidInvitationPublicURL = &invitationConfigurationError{}

type invitationConfigurationError struct{}

func (*invitationConfigurationError) Error() string { return "invalid invitation public URL" }

func parseInvitationPublicURL(raw string) (*url.URL, error) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return nil, errInvalidInvitationPublicURL
	}
	for _, char := range raw {
		if unicode.IsControl(char) {
			return nil, errInvalidInvitationPublicURL
		}
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Opaque != "" || parsed.Host == "" || parsed.Hostname() == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery ||
		parsed.Fragment != "" || parsed.RawFragment != "" ||
		strings.ContainsAny(raw, "?#") ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawPath != "" {
		return nil, errInvalidInvitationPublicURL
	}
	return &url.URL{Scheme: parsed.Scheme, Host: parsed.Host}, nil
}

func (delivery invitationDelivery) invitationURL(token string) string {
	link := *delivery.publicURL
	link.Path = "/invite/" + token
	link.RawPath = "/invite/" + url.PathEscape(token)
	return link.String()
}

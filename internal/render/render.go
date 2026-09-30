// Package render renders stored plain-text Page values with HTML escaping.
package render

import (
	"bytes"
	_ "embed"
	"html/template"

	"github.com/markdlabrecque/composure/internal/content"
)

//go:embed page.html
var source string
var page = template.Must(template.New("page").Parse(source))

func Page(snapshot content.Snapshot) ([]byte, error) {
	return renderPage(snapshot, false)
}

// Preview renders the public Page template with preview context outside the
// shared content region.
func Preview(snapshot content.Snapshot) ([]byte, error) {
	return renderPage(snapshot, true)
}

func renderPage(snapshot content.Snapshot, preview bool) ([]byte, error) {
	var out bytes.Buffer
	err := page.Execute(&out, struct {
		content.Snapshot
		Preview bool
	}{Snapshot: snapshot, Preview: preview})
	return out.Bytes(), err
}

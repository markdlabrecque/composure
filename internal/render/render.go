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
	var out bytes.Buffer
	err := page.Execute(&out, snapshot)
	return out.Bytes(), err
}

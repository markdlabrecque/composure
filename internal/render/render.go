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
	return renderPage(snapshot, false, content.PageDefinition{Fields: []content.FieldDefinition{{ID: "body", Kind: "long_text"}}})
}

// PageWithDefinition renders configured fields in definition order.
func PageWithDefinition(snapshot content.Snapshot, definition content.PageDefinition) ([]byte, error) {
	return renderPage(snapshot, false, definition)
}

// Preview renders the public Page template with preview context outside the
// shared content region.
func Preview(snapshot content.Snapshot) ([]byte, error) {
	return renderPage(snapshot, true, content.PageDefinition{Fields: []content.FieldDefinition{{ID: "body", Kind: "long_text"}}})
}

// PreviewWithDefinition renders a saved draft with the site's active fields.
func PreviewWithDefinition(snapshot content.Snapshot, definition content.PageDefinition) ([]byte, error) {
	return renderPage(snapshot, true, definition)
}

func renderPage(snapshot content.Snapshot, preview bool, definition content.PageDefinition) ([]byte, error) {
	type fieldValue struct{ ID, Kind, Value string }
	fields := make([]fieldValue, 0, len(definition.Fields))
	for _, field := range definition.Fields {
		fields = append(fields, fieldValue{ID: field.ID, Kind: field.Kind, Value: snapshot.Fields[field.ID]})
	}
	var out bytes.Buffer
	err := page.Execute(&out, struct {
		content.Snapshot
		Preview    bool
		PageFields []fieldValue
	}{Snapshot: snapshot, Preview: preview, PageFields: fields})
	return out.Bytes(), err
}

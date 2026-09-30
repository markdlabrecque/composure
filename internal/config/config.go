// Package config owns the closed Page configuration document.
package config

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
)

//go:embed default.json
var Default []byte

const FormatVersion = 1

type Document struct {
	FormatVersion int    `json:"format_version"`
	ContentTypes  []Type `json:"content_types"`
}
type Type struct {
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	Fields []Field `json:"fields"`
}
type Field struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	HelpText string `json:"help_text"`
	Required bool   `json:"required"`
	Order    int    `json:"order"`
}

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Decode validates the stored active configuration. It does not import files.
func Decode(data []byte) (Document, error) {
	var document Document
	if err := uniqueProperties(json.NewDecoder(bytes.NewReader(data))); err != nil {
		return document, err
	}
	// Inspect raw objects first to enforce required properties and scalar types.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return document, err
	}
	if err := keys(raw, "format_version", "content_types"); err != nil {
		return document, err
	}
	var types []map[string]json.RawMessage
	if err := json.Unmarshal(raw["content_types"], &types); err != nil {
		return document, err
	}
	for _, typ := range types {
		if err := keys(typ, "id", "label", "fields"); err != nil {
			return document, err
		}
		var fields []map[string]json.RawMessage
		if err := json.Unmarshal(typ["fields"], &fields); err != nil {
			return document, err
		}
		for _, field := range fields {
			if err := keys(field, "id", "kind", "label", "help_text", "required", "order"); err != nil {
				return document, err
			}
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return document, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return document, fmt.Errorf("expected one JSON document")
	}
	if document.FormatVersion != 1 {
		return document, fmt.Errorf("unsupported configuration format %d", document.FormatVersion)
	}
	if len(document.ContentTypes) != 1 || document.ContentTypes[0].ID != "page" {
		return document, fmt.Errorf("exactly one page content type is required")
	}
	ids := map[string]bool{}
	orders := map[int]bool{}
	for _, f := range document.ContentTypes[0].Fields {
		if !identifier.MatchString(f.ID) || f.ID == "title" || f.ID == "path" || ids[f.ID] {
			return document, fmt.Errorf("invalid or duplicate field ID %q", f.ID)
		}
		if f.Kind != "short_text" && f.Kind != "long_text" {
			return document, fmt.Errorf("unsupported field kind %q", f.Kind)
		}
		if f.Order <= 0 || orders[f.Order] {
			return document, fmt.Errorf("invalid or duplicate field order %d", f.Order)
		}
		ids[f.ID] = true
		orders[f.Order] = true
	}
	return document, nil
}

// encoding/json otherwise accepts duplicate properties using the last value.
func uniqueProperties(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	seen := map[string]bool{}
	for decoder.More() {
		if delimiter == '{' {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return fmt.Errorf("duplicate or invalid configuration property %q", name)
			}
			seen[name] = true
		}
		if err := uniqueProperties(decoder); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func keys(object map[string]json.RawMessage, names ...string) error {
	if len(object) != len(names) {
		return fmt.Errorf("configuration object has missing or unknown properties")
	}
	for _, name := range names {
		value, ok := object[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("configuration property %s is required", name)
		}
	}
	return nil
}

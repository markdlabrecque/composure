// Package config owns the closed Page configuration document.
package config

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"strings"
	"unicode/utf8"
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
	ID       string      `json:"id"`
	Kind     string      `json:"kind"`
	Label    string      `json:"label"`
	HelpText string      `json:"help_text"`
	Required bool        `json:"required"`
	Order    json.Number `json:"order"`
}

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Decode validates the stored active configuration. It does not import files.
func Decode(data []byte) (Document, error) {
	var document Document
	if !utf8.Valid(data) {
		return document, fmt.Errorf("invalid configuration JSON: input is not valid UTF-8")
	}
	if !json.Valid(data) {
		return document, fmt.Errorf("invalid configuration JSON")
	}
	if err := validateSurrogateEscapes(data); err != nil {
		return document, err
	}
	if err := uniqueProperties(json.NewDecoder(bytes.NewReader(data))); err != nil {
		return document, err
	}
	// Inspect the format marker before validating the v1 shape so an otherwise
	// well-formed document from another version gets actionable guidance.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return document, err
	}
	if raw == nil {
		return document, fmt.Errorf("configuration root must be an object")
	}
	versionRaw, ok := raw["format_version"]
	if !ok || bytes.Equal(bytes.TrimSpace(versionRaw), []byte("null")) {
		return document, fmt.Errorf("configuration property format_version is required")
	}
	version, ok := integer(versionRaw)
	if !ok {
		return document, fmt.Errorf("configuration format version must be an integer")
	}
	if version.Cmp(big.NewInt(1)) != 0 {
		return document, unsupportedFormatVersion(version.String())
	}
	// Inspect raw objects first to enforce required properties and scalar types.
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
			if _, valid := integer(field["order"]); !valid {
				return document, fmt.Errorf("configuration field order must be an integer")
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
	if len(document.ContentTypes) != 1 || document.ContentTypes[0].ID != "page" {
		return document, fmt.Errorf("exactly one page content type is required")
	}
	ids := map[string]bool{}
	orders := map[string]bool{}
	for _, f := range document.ContentTypes[0].Fields {
		if !identifier.MatchString(f.ID) || f.ID == "title" || f.ID == "path" || ids[f.ID] {
			return document, fmt.Errorf("invalid or duplicate field ID %q", f.ID)
		}
		if f.Kind != "short_text" && f.Kind != "long_text" {
			return document, fmt.Errorf("unsupported field kind %q", f.Kind)
		}
		order, valid := integer([]byte(f.Order))
		if !valid || order.Sign() <= 0 || orders[order.String()] {
			return document, fmt.Errorf("invalid or duplicate field order %s", f.Order)
		}
		ids[f.ID] = true
		orders[order.String()] = true
	}
	return document, nil
}

func integer(raw []byte) (*big.Int, bool) {
	text := string(bytes.TrimSpace(raw))
	if text == "" || strings.ContainsAny(text, ".eE") {
		return nil, false
	}
	n, ok := new(big.Int).SetString(text, 10)
	return n, ok
}

func unsupportedFormatVersion(version string) error {
	n, _ := new(big.Int).SetString(version, 10)
	if n.Sign() <= 0 {
		return fmt.Errorf("configuration format version %s is older than supported 1; a migration is required, which phase 1 does not provide", version)
	}
	return fmt.Errorf("configuration format version %s is newer than supported 1; use a newer composure binary", version)
}

// encoding/json replaces malformed UTF-8 and unpaired UTF-16 escapes with
// U+FFFD. The active document contract permits only Unicode scalar values.
func validateSurrogateEscapes(data []byte) error {
	inString := false
	for i := 0; i < len(data); i++ {
		if !inString {
			if data[i] == '"' {
				inString = true
			}
			continue
		}
		if data[i] == '"' {
			inString = false
			continue
		}
		if data[i] != '\\' {
			continue
		}
		if i+1 >= len(data) {
			break // json.Valid reports the incomplete escape.
		}
		if data[i+1] != 'u' {
			i++ // skip the escaped byte, including an escaped backslash.
			continue
		}
		if i+6 > len(data) {
			break // json.Valid reports the incomplete Unicode escape.
		}
		unit, ok := hexUnit(data[i+2 : i+6])
		if !ok {
			i += 5 // json.Valid reports non-hex escape digits.
			continue
		}
		switch {
		case unit >= 0xD800 && unit <= 0xDBFF:
			if i+12 > len(data) || data[i+6] != '\\' || data[i+7] != 'u' {
				return fmt.Errorf("invalid configuration JSON: unpaired high surrogate at byte %d", i)
			}
			low, valid := hexUnit(data[i+8 : i+12])
			if !valid || low < 0xDC00 || low > 0xDFFF {
				return fmt.Errorf("invalid configuration JSON: unpaired high surrogate at byte %d", i)
			}
			i += 11
		case unit >= 0xDC00 && unit <= 0xDFFF:
			return fmt.Errorf("invalid configuration JSON: unpaired low surrogate at byte %d", i)
		default:
			i += 5
		}
	}
	return nil
}

func hexUnit(digits []byte) (uint16, bool) {
	var value uint16
	for _, digit := range digits {
		value <<= 4
		switch {
		case digit >= '0' && digit <= '9':
			value |= uint16(digit - '0')
		case digit >= 'a' && digit <= 'f':
			value |= uint16(digit-'a') + 10
		case digit >= 'A' && digit <= 'F':
			value |= uint16(digit-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

// encoding/json otherwise accepts duplicate properties using the last value.
func uniqueProperties(decoder *json.Decoder) error {
	decoder.UseNumber()
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

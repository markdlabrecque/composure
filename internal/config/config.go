// Package config owns the closed Page configuration document.
package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math/big"
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
	ID       string      `json:"id"`
	Kind     string      `json:"kind"`
	Label    string      `json:"label"`
	HelpText string      `json:"help_text"`
	Required bool        `json:"required"`
	Order    json.Number `json:"order"`
}

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Decode validates a stored active configuration. It shares the validator
// used by the CLI and preserves the established startup error wording.
func Decode(data []byte) (Document, error) {
	document, err := Validate(data)
	if validation, ok := err.(*ValidationError); ok && validation.Legacy != "" {
		return document, fmt.Errorf("%s", validation.Legacy)
	}
	return document, err
}

func integerNumber(number json.Number) (*big.Int, bool) {
	text := string(number)
	if text == "" {
		return nil, false
	}
	for i, r := range text {
		if r == '-' && i == 0 {
			continue
		}
		if r < '0' || r > '9' {
			return nil, false
		}
	}
	if text == "-" {
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

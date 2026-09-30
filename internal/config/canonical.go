package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// Canonical returns the deterministic v1 representation of a validated Page
// configuration document.
func Canonical(document Document) ([]byte, error) {
	canonical := document
	canonical.ContentTypes = append([]Type(nil), document.ContentTypes...)
	sort.Slice(canonical.ContentTypes, func(i, j int) bool {
		return canonical.ContentTypes[i].ID < canonical.ContentTypes[j].ID
	})
	for typeIndex := range canonical.ContentTypes {
		fields := append([]Field{}, canonical.ContentTypes[typeIndex].Fields...)
		canonical.ContentTypes[typeIndex].Fields = fields
		for _, field := range fields {
			if _, valid := integerNumber(field.Order); !valid {
				return nil, fmt.Errorf("field order is not an integer")
			}
		}
		sort.Slice(fields, func(i, j int) bool {
			left, _ := integerNumber(fields[i].Order)
			right, _ := integerNumber(fields[j].Order)
			if comparison := left.Cmp(right); comparison != 0 {
				return comparison < 0
			}
			return fields[i].ID < fields[j].ID
		})
	}
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(canonical); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

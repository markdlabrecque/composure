package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ValidationError is a stable, path-aware configuration diagnostic.
type ValidationError struct {
	Class    string
	Path     string
	Message  string
	ExitCode int
	Legacy   string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s at %s: %s", e.Class, e.Path, e.Message)
}

type jsonKind uint8

const (
	jsonNull jsonKind = iota
	jsonBool
	jsonNumber
	jsonStringKind
	jsonArray
	jsonObject
)

type jsonValue struct {
	kind   jsonKind
	text   string
	array  []*jsonValue
	object []jsonMember
}

type jsonMember struct {
	name  string
	value *jsonValue
}

var simplePathName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Validate checks one closed Page configuration document. It preserves JSON
// number spelling until validation has proved that integer fields are exact.
func Validate(data []byte) (Document, error) {
	var empty Document
	var syntacticDocument json.RawMessage
	if err := json.Unmarshal(data, &syntacticDocument); err != nil {
		return empty, wholeDocumentSyntaxFailure(data, err)
	}
	if offset := invalidUTF8Offset(data); offset >= 0 {
		return empty, syntaxFailure(offset, "input is not valid UTF-8", "invalid configuration JSON: input is not valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var duplicate *ValidationError
	root, err := decodeValue(decoder, "$", &duplicate)
	if err != nil {
		return empty, decoderFailure(data, decoder, err)
	}
	if offset := unpairedSurrogateOffset(data); offset >= 0 {
		legacy := "invalid configuration JSON: unpaired surrogate at byte " + strconv.Itoa(offset)
		return empty, syntaxFailure(offset, "unpaired UTF-16 surrogate escape", legacy)
	}
	if duplicate != nil {
		return empty, duplicate
	}
	if err := validateRoot(root); err != nil {
		return empty, err
	}
	var document Document
	if err := json.Unmarshal(data, &document); err != nil {
		return empty, validationError("invalid_type", "$", "configuration values have the wrong type", 3, "")
	}
	return document, nil
}

func decodeValue(decoder *json.Decoder, path string, duplicate **ValidationError) (*jsonValue, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			result := &jsonValue{kind: jsonObject}
			seen := make(map[string]bool)
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				name, ok := key.(string)
				if !ok {
					return nil, fmt.Errorf("invalid object member")
				}
				childPath := memberPath(path, name)
				if seen[name] && *duplicate == nil {
					*duplicate = validationError("duplicate_property", childPath,
						"duplicate property "+jsonString(name), 3,
						fmt.Sprintf("duplicate or invalid configuration property %q", name))
				}
				seen[name] = true
				child, err := decodeValue(decoder, childPath, duplicate)
				if err != nil {
					return nil, err
				}
				result.object = append(result.object, jsonMember{name, child})
			}
			close, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			if close != json.Delim('}') {
				return nil, fmt.Errorf("invalid object terminator")
			}
			return result, nil
		case '[':
			result := &jsonValue{kind: jsonArray}
			for index := 0; decoder.More(); index++ {
				child, err := decodeValue(decoder, fmt.Sprintf("%s[%d]", path, index), duplicate)
				if err != nil {
					return nil, err
				}
				result.array = append(result.array, child)
			}
			close, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			if close != json.Delim(']') {
				return nil, fmt.Errorf("invalid array terminator")
			}
			return result, nil
		default:
			return nil, fmt.Errorf("unexpected JSON delimiter")
		}
	case nil:
		return &jsonValue{kind: jsonNull}, nil
	case bool:
		return &jsonValue{kind: jsonBool, text: strconv.FormatBool(value)}, nil
	case json.Number:
		return &jsonValue{kind: jsonNumber, text: string(value)}, nil
	case string:
		return &jsonValue{kind: jsonStringKind, text: value}, nil
	default:
		return nil, fmt.Errorf("unsupported JSON token")
	}
}

func decoderFailure(data []byte, decoder *json.Decoder, err error) *ValidationError {
	offset := int(decoder.InputOffset())
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		offset = int(syntax.Offset) - 1
		if int(syntax.Offset) >= len(data) && strings.Contains(strings.ToLower(syntax.Error()), "unexpected") {
			offset = len(data)
		}
	} else if err == io.EOF || err == io.ErrUnexpectedEOF {
		offset = len(data)
	}
	if offset < 0 {
		offset = 0
	}
	return syntaxFailure(offset, "invalid JSON syntax", "invalid configuration JSON")
}

func wholeDocumentSyntaxFailure(data []byte, err error) *ValidationError {
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		offset := int(syntax.Offset) - 1
		diagnostic := strings.ToLower(syntax.Error())
		if strings.Contains(diagnostic, "invalid escape sequence") && strings.Contains(diagnostic, `\u`) {
			end := int(syntax.Offset)
			if end > len(data) {
				end = len(data)
			}
			if invalidHex := invalidUnicodeHexOffset(data[:end]); invalidHex >= 0 {
				offset = invalidHex
			}
		}
		if strings.Contains(strings.ToLower(syntax.Error()), "unexpected end") {
			offset = len(data)
		}
		return syntaxFailure(offset, "invalid JSON syntax", "invalid configuration JSON")
	}
	return syntaxFailure(len(data), "invalid JSON syntax", "invalid configuration JSON")
}

func validateRoot(root *jsonValue) *ValidationError {
	if root == nil || root.kind != jsonObject {
		return validationError("invalid_type", "$", "configuration root must be an object", 3, "configuration root must be an object")
	}
	version, found := objectValue(root, "format_version")
	if !found {
		return validationError("missing_property", "$.format_version", "required property \"format_version\" is missing", 3, "configuration property format_version is required")
	}
	if version.kind != jsonNumber {
		return validationError("invalid_type", "$.format_version", "format_version must be a JSON integer", 3, "configuration format version must be an integer")
	}
	versionInt, integer := integerNumber(json.Number(version.text))
	if !integer {
		return validationError("invalid_type", "$.format_version", "format_version must be a JSON integer", 3, "configuration format version must be an integer")
	}
	if versionInt.Cmp(big.NewInt(FormatVersion)) != 0 {
		legacy := unsupportedFormatVersion(versionInt.String()).Error()
		return validationError("unsupported_version", "$.format_version", legacy, 4, legacy)
	}
	if err := checkRequired(root, "$", []string{"format_version", "content_types"}); err != nil {
		return err
	}
	if err := checkUnknown(root, "$", []string{"format_version", "content_types"}); err != nil {
		return err
	}
	contentTypes, _ := objectValue(root, "content_types")
	if contentTypes.kind != jsonArray {
		return validationError("invalid_type", "$.content_types", "content_types must be an array", 3, "exactly one page content type is required")
	}
	if len(contentTypes.array) == 0 {
		return validationError("unsupported_content_type", "$.content_types", "exactly one Page content type is required", 3, "exactly one page content type is required")
	}
	if err := validateType(contentTypes.array[0], "$.content_types[0]"); err != nil {
		return err
	}
	if len(contentTypes.array) > 1 {
		return validationError("unsupported_content_type", "$.content_types[1]", "additional content types are not supported", 3, "exactly one page content type is required")
	}
	return nil
}

func validateType(value *jsonValue, path string) *ValidationError {
	if value == nil || value.kind != jsonObject {
		return validationError("invalid_type", path, "content type must be an object", 3, "exactly one page content type is required")
	}
	if err := checkRequired(value, path, []string{"id", "label", "fields"}); err != nil {
		return err
	}
	if err := checkUnknown(value, path, []string{"id", "label", "fields"}); err != nil {
		return err
	}
	id, _ := objectValue(value, "id")
	if id.kind != jsonStringKind {
		return validationError("invalid_type", memberPath(path, "id"), "content type id must be a string", 3, "exactly one page content type is required")
	}
	if id.text != "page" {
		return validationError("unsupported_content_type", memberPath(path, "id"), "only the Page content type is supported", 3, "exactly one page content type is required")
	}
	label, _ := objectValue(value, "label")
	if label.kind != jsonStringKind {
		return validationError("invalid_type", memberPath(path, "label"), "content type label must be a string", 3, "exactly one page content type is required")
	}
	fields, _ := objectValue(value, "fields")
	if fields.kind != jsonArray {
		return validationError("invalid_type", memberPath(path, "fields"), "fields must be an array", 3, "exactly one page content type is required")
	}
	ids := make(map[string]bool)
	orders := make(map[string]bool)
	for index, field := range fields.array {
		fieldPath := fmt.Sprintf("%s[%d]", memberPath(path, "fields"), index)
		id, order, err := validateField(field, fieldPath)
		if err != nil {
			return err
		}
		if ids[id] {
			return validationError("duplicate_id", memberPath(fieldPath, "id"), "field id "+jsonString(id)+" is duplicated", 3, fmt.Sprintf("invalid or duplicate field ID %q", id))
		}
		if orders[order] {
			return validationError("duplicate_order", memberPath(fieldPath, "order"), "field order "+order+" is duplicated", 3, "invalid or duplicate field order "+order)
		}
		ids[id] = true
		orders[order] = true
	}
	return nil
}

func validateField(value *jsonValue, path string) (string, string, *ValidationError) {
	if value == nil || value.kind != jsonObject {
		return "", "", validationError("invalid_type", path, "field must be an object", 3, "configuration object has missing or unknown properties")
	}
	properties := []string{"id", "kind", "label", "help_text", "required", "order"}
	if err := checkRequired(value, path, properties); err != nil {
		return "", "", err
	}
	if err := checkUnknown(value, path, properties); err != nil {
		return "", "", err
	}
	id, _ := objectValue(value, "id")
	if id.kind != jsonStringKind {
		return "", "", validationError("invalid_type", memberPath(path, "id"), "field id must be a string", 3, "invalid or duplicate field ID")
	}
	if id.text == "title" || id.text == "path" {
		return "", "", validationError("reserved_id", memberPath(path, "id"), "field id "+jsonString(id.text)+" is reserved", 3, fmt.Sprintf("invalid or duplicate field ID %q", id.text))
	}
	if !identifier.MatchString(id.text) {
		return "", "", validationError("invalid_id", memberPath(path, "id"), "field id must match [a-z][a-z0-9_]{0,63}", 3, fmt.Sprintf("invalid or duplicate field ID %q", id.text))
	}
	kind, _ := objectValue(value, "kind")
	if kind.kind != jsonStringKind {
		return "", "", validationError("invalid_type", memberPath(path, "kind"), "field kind must be a string", 3, "unsupported field kind")
	}
	if kind.text != "short_text" && kind.text != "long_text" {
		return "", "", validationError("invalid_value", memberPath(path, "kind"), "field kind must be \"short_text\" or \"long_text\"", 3, fmt.Sprintf("unsupported field kind %q", kind.text))
	}
	label, _ := objectValue(value, "label")
	if label.kind != jsonStringKind {
		return "", "", validationError("invalid_type", memberPath(path, "label"), "field label must be a string", 3, "configuration object has missing or unknown properties")
	}
	help, _ := objectValue(value, "help_text")
	if help.kind != jsonStringKind {
		return "", "", validationError("invalid_type", memberPath(path, "help_text"), "field help_text must be a string", 3, "configuration object has missing or unknown properties")
	}
	required, _ := objectValue(value, "required")
	if required.kind != jsonBool {
		return "", "", validationError("invalid_type", memberPath(path, "required"), "field required must be a boolean", 3, "configuration object has missing or unknown properties")
	}
	order, _ := objectValue(value, "order")
	if order.kind != jsonNumber {
		return "", "", validationError("invalid_type", memberPath(path, "order"), "field order must be a JSON integer", 3, "configuration field order must be an integer")
	}
	orderInt, valid := integerNumber(json.Number(order.text))
	if !valid {
		return "", "", validationError("invalid_type", memberPath(path, "order"), "field order must be a JSON integer", 3, "configuration field order must be an integer")
	}
	if orderInt.Sign() <= 0 {
		return "", "", validationError("invalid_value", memberPath(path, "order"), "field order must be positive", 3, "invalid or duplicate field order "+order.text)
	}
	return id.text, orderInt.String(), nil
}

func checkRequired(value *jsonValue, path string, properties []string) *ValidationError {
	for _, property := range properties {
		if _, ok := objectValue(value, property); !ok {
			return validationError("missing_property", memberPath(path, property), "required property "+jsonString(property)+" is missing", 3, "configuration property "+property+" is required")
		}
	}
	return nil
}

func checkUnknown(value *jsonValue, path string, properties []string) *ValidationError {
	known := make(map[string]bool, len(properties))
	for _, property := range properties {
		known[property] = true
	}
	for _, member := range value.object {
		if !known[member.name] {
			return validationError("unknown_property", memberPath(path, member.name), "unknown property "+jsonString(member.name), 3, "configuration object has missing or unknown properties")
		}
	}
	return nil
}

func objectValue(value *jsonValue, name string) (*jsonValue, bool) {
	if value == nil || value.kind != jsonObject {
		return nil, false
	}
	for _, member := range value.object {
		if member.name == name {
			return member.value, true
		}
	}
	return nil, false
}

func validationError(class, path, message string, exitCode int, legacy string) *ValidationError {
	return &ValidationError{Class: class, Path: path, Message: message, ExitCode: exitCode, Legacy: legacy}
}

func syntaxFailure(offset int, message, legacy string) *ValidationError {
	return validationError("json_syntax", "$", fmt.Sprintf("invalid JSON at byte %d: %s", offset, message), 3, legacy)
}

func memberPath(path, name string) string {
	if simplePathName.MatchString(name) {
		return path + "." + name
	}
	return path + "[" + jsonString(name) + "]"
}

func jsonString(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func invalidUTF8Offset(data []byte) int {
	for offset := 0; offset < len(data); {
		r, size := utf8.DecodeRune(data[offset:])
		if r == utf8.RuneError && size == 1 && data[offset] >= utf8.RuneSelf {
			return offset
		}
		offset += size
	}
	return -1
}

func isJSONSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n'
}

func unpairedSurrogateOffset(data []byte) int {
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
		if data[i] != '\\' || i+1 >= len(data) {
			continue
		}
		if data[i+1] != 'u' || i+6 > len(data) {
			i++
			continue
		}
		unit, ok := hexUnit(data[i+2 : i+6])
		if !ok {
			i += 5
			continue
		}
		switch {
		case unit >= 0xD800 && unit <= 0xDBFF:
			if i+12 > len(data) || data[i+6] != '\\' || data[i+7] != 'u' {
				return i
			}
			low, valid := hexUnit(data[i+8 : i+12])
			if !valid || low < 0xDC00 || low > 0xDFFF {
				return i
			}
			i += 11
		case unit >= 0xDC00 && unit <= 0xDFFF:
			return i
		default:
			i += 5
		}
	}
	return -1
}

func invalidUnicodeHexOffset(data []byte) int {
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
		if data[i] != '\\' || i+1 >= len(data) {
			continue
		}
		if data[i+1] != 'u' {
			i++
			continue
		}
		if i+6 > len(data) {
			return -1
		}
		for digit := i + 2; digit < i+6; digit++ {
			if !isHexDigit(data[digit]) {
				return digit
			}
		}
		i += 5
	}
	return -1
}

func isHexDigit(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F'
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

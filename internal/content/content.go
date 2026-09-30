// Package content defines persisted public content, draft validation, and the repository boundary.
package content

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrNotFound = errors.New("content not found")
var ErrPathTaken = errors.New("path is already owned")
var ErrStaleDraft = errors.New("draft revision is stale")
var ErrPathChangeUnsupported = errors.New("published Page path changes are unsupported")

type PathChangeUnsupportedError struct {
	OwnedPath string
}

func (e *PathChangeUnsupportedError) Error() string {
	return fmt.Sprintf("published Page path change is unsupported while %s is owned", e.OwnedPath)
}

func (e *PathChangeUnsupportedError) Unwrap() error { return ErrPathChangeUnsupported }

const (
	MaxTitleLength = 200
	MaxBodyLength  = 100000
	MaxShortLength = 255
)

// Snapshot contains the immutable values visible to visitors.
type Snapshot struct {
	ID, ItemID, Title, Path string
	Fields                  map[string]string
}

type ActiveConfig struct {
	Document []byte
	Revision int64
}

type ItemDraft struct {
	Title  string
	Path   string
	Fields map[string]string
}

type Item struct {
	ID, TypeID, Title, Path string
	Fields                  map[string]string
	Revision                int
	CreatedAt, UpdatedAt    string
	Published               bool
}

type ItemSummary struct {
	ID, Title, Path, State, UpdatedAt string
}

type PathTakenError struct {
	OwnerID    string
	OwnerTitle string
}

func (e *PathTakenError) Error() string { return ErrPathTaken.Error() }
func (e *PathTakenError) Unwrap() error { return ErrPathTaken }

type FieldDefinition struct {
	ID, Kind, Label, HelpText string
	Required                  bool
	Order                     string
}

type PageDefinition struct {
	Revision int64
	Fields   []FieldDefinition
}

type FieldError struct {
	Field string
	Code  string
}

// Repository resolves public snapshots and owns bounded Page draft operations.
type Repository interface {
	PublishedByPath(context.Context, string) (Snapshot, error)
	ActiveConfig(context.Context) (ActiveConfig, error)
	ListItems(context.Context, string) ([]ItemSummary, error)
	GetItem(context.Context, string) (Item, error)
	CreateItem(context.Context, ItemDraft, time.Time) (string, error)
	SaveDraft(context.Context, string, int, ItemDraft, time.Time, string) (changed bool, err error)
}

var pagePath = regexp.MustCompile(`^/$|^(/[a-z0-9]+(-[a-z0-9]+)*){1,8}$`)
var reservedPathSegments = map[string]bool{
	"admin": true, "static": true, "media": true, "files": true, "healthz": true, "api": true,
}

// PreparePageDraft normalizes system values, validates configured fields, and
// drops form keys that are not part of the active Page definition.
func PreparePageDraft(definition PageDefinition, submitted map[string]string) (ItemDraft, []FieldError) {
	draft := ItemDraft{Fields: make(map[string]string, len(definition.Fields))}
	var problems []FieldError
	draft.Title = strings.TrimSpace(submitted["title"])
	if draft.Title == "" {
		problems = append(problems, FieldError{Field: "title", Code: "required"})
	} else if runeLength(draft.Title) > MaxTitleLength {
		problems = append(problems, FieldError{Field: "title", Code: "too_long"})
	} else if hasControl(draft.Title) {
		problems = append(problems, FieldError{Field: "title", Code: "invalid_text"})
	}

	var pathCode string
	rawPath := strings.TrimSpace(submitted["path"])
	if rawPath == "" {
		pathCode = "required"
	} else {
		draft.Path = NormalizePath(rawPath)
		if len(draft.Path) > 200 || !pagePath.MatchString(draft.Path) {
			pathCode = "path_invalid"
		} else if reservedPathSegments[firstPathSegment(draft.Path)] {
			pathCode = "path_reserved"
		}
	}
	if pathCode != "" {
		problems = append(problems, FieldError{Field: "path", Code: pathCode})
	}

	for _, field := range definition.Fields {
		value := submitted[field.ID]
		if field.Kind == "long_text" {
			value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
		}
		if field.Required && strings.TrimSpace(value) == "" {
			problems = append(problems, FieldError{Field: field.ID, Code: "required"})
		}
		limit := MaxBodyLength
		if field.Kind == "short_text" {
			limit = MaxShortLength
		}
		if runeLength(value) > limit {
			problems = append(problems, FieldError{Field: field.ID, Code: "too_long"})
		}
		draft.Fields[field.ID] = value
	}
	return draft, problems
}

// NormalizePath applies the v1 path normalization rules before validation.
func NormalizePath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if len(path) > 1 && strings.HasSuffix(path, "/") {
		path = strings.TrimSuffix(path, "/")
	}
	return path
}

func firstPathSegment(path string) string {
	if path == "/" {
		return ""
	}
	return strings.Split(strings.TrimPrefix(path, "/"), "/")[0]
}

func runeLength(value string) int { return utf8.RuneCountInString(value) }

func hasControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// SortPageFields sorts definitions by their exact positive integer order.
func SortPageFields(fields []FieldDefinition) {
	// Configuration decoding has already validated each order as an integer.
	sort.Slice(fields, func(i, j int) bool {
		left, _ := new(big.Int).SetString(fields[i].Order, 10)
		right, _ := new(big.Int).SetString(fields[j].Order, 10)
		if compare := left.Cmp(right); compare != 0 {
			return compare < 0
		}
		return fields[i].ID < fields[j].ID
	})
}

// NewID generates a UUIDv7 using the supplied clock value and secure randomness.
func NewID(at time.Time) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	ms := uint64(at.UnixMilli())
	for i := 5; i >= 0; i-- {
		b[i] = byte(ms)
		ms >>= 8
	}
	b[6] = (b[6] & 0x0f) | 0x70
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

package upload

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"path"
	"strings"
	"unicode/utf8"
)

const (
	maxDOCXEntries       = 1000
	maxDOCXEntryBytes    = 25 * 1024 * 1024
	maxDOCXExpandedBytes = 100 * 1024 * 1024
	maxDOCXRatio         = 100
	docxUnixCreator      = 3
	docxUnixTypeMask     = 0170000
	docxUnixRegular      = 0100000
	docxUnixDirectory    = 0040000
)

const (
	docxContentTypesPath = "[Content_Types].xml"
	docxRelationships    = "_rels/.rels"
	docxMainContentType  = "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"
	docxContentNS        = "http://schemas.openxmlformats.org/package/2006/content-types"
	docxRelationshipsNS  = "http://schemas.openxmlformats.org/package/2006/relationships"
	docxOfficeRelType    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
	docxWordNS           = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
)

// ValidateDOCX checks that data is a bounded, non-macro OOXML Word document.
// It reads archive members in memory and never extracts them to disk.
func ValidateDOCX(data []byte) error {
	if len(data) == 0 {
		return errors.New("DOCX archive is empty")
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("read DOCX ZIP archive: %w", err)
	}
	if len(r.File) == 0 || len(r.File) > maxDOCXEntries {
		return fmt.Errorf("DOCX ZIP entry count must be between 1 and %d", maxDOCXEntries)
	}

	files := make(map[string]*zip.File, len(r.File))
	identities := make(map[string]bool, len(r.File))
	var declaredExpanded, declaredCompressed uint64
	for _, f := range r.File {
		name, isDir, err := validateDOCXPath(f)
		if err != nil {
			return err
		}
		identity := strings.TrimSuffix(name, "/")
		if identities[identity] {
			return fmt.Errorf("duplicate DOCX ZIP path identity %q", identity)
		}
		identities[identity] = true
		if _, exists := files[name]; exists {
			return fmt.Errorf("duplicate DOCX ZIP path %q", name)
		}
		files[name] = f
		if strings.Contains(strings.ToLower(name), "vbaproject.bin") {
			return errors.New("macro-bearing DOCX archives are not accepted")
		}
		if f.Method != zip.Store && f.Method != zip.Deflate {
			return fmt.Errorf("unsupported DOCX ZIP compression method %d", f.Method)
		}
		if err := validateDOCXFlags(f); err != nil {
			return err
		}
		if isDir && f.UncompressedSize64 != 0 {
			return fmt.Errorf("DOCX directory %q contains data", name)
		}
		if f.UncompressedSize64 > maxDOCXEntryBytes {
			return fmt.Errorf("DOCX ZIP entry %q exceeds the expanded-size limit", name)
		}
		if f.CompressedSize64 > uint64(len(data)) {
			return fmt.Errorf("DOCX ZIP entry %q has inconsistent compressed-size metadata", name)
		}
		if declaredExpanded > math.MaxUint64-f.UncompressedSize64 || declaredCompressed > math.MaxUint64-f.CompressedSize64 {
			return errors.New("DOCX ZIP size metadata overflows")
		}
		declaredExpanded += f.UncompressedSize64
		declaredCompressed += f.CompressedSize64
		if declaredExpanded > maxDOCXExpandedBytes {
			return errors.New("DOCX archive exceeds the total expanded-size limit")
		}
		if exceedsDOCXRatio(f.UncompressedSize64, f.CompressedSize64) {
			return fmt.Errorf("DOCX ZIP entry %q exceeds the compression-ratio limit", name)
		}
	}
	if exceedsDOCXRatio(declaredExpanded, declaredCompressed) {
		return errors.New("DOCX archive exceeds the overall compression-ratio limit")
	}

	// Read the two package-level XML parts first so the relationship identifies
	// the main part before the remaining members are read. Each member is then
	// expanded exactly once, keeping the actual total work within the archive
	// limit even when the main document is near the per-entry maximum.
	memberData := make(map[string][]byte, 3)
	var actualExpanded, actualCompressed uint64
	readMember := func(name string, f *zip.File, keep bool) ([]byte, error) {
		if f.UncompressedSize64 > math.MaxInt64 {
			return nil, fmt.Errorf("DOCX ZIP entry %q is too large to read", name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("open DOCX ZIP entry %q: %w", name, err)
		}
		var dst io.Writer = io.Discard
		var buf bytes.Buffer
		if keep {
			dst = &buf
		}
		readLimit := uint64(maxDOCXEntryBytes + 1)
		archiveRemaining := uint64(maxDOCXExpandedBytes) - actualExpanded
		if archiveRemaining < readLimit-1 {
			readLimit = archiveRemaining + 1
		}
		limited := &io.LimitedReader{R: rc, N: int64(readLimit)}
		written, copyErr := io.Copy(dst, limited)
		closeErr := rc.Close()
		if copyErr != nil {
			return nil, fmt.Errorf("read DOCX ZIP entry %q: %w", name, copyErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close DOCX ZIP entry %q: %w", name, closeErr)
		}
		if uint64(written) > archiveRemaining {
			return nil, errors.New("DOCX archive exceeds the total expanded-size limit")
		}
		if written > maxDOCXEntryBytes || uint64(written) != f.UncompressedSize64 {
			return nil, fmt.Errorf("DOCX ZIP entry %q has inconsistent expanded-size metadata", name)
		}
		if strings.HasSuffix(name, "/") && written != 0 {
			return nil, fmt.Errorf("DOCX directory %q contains data", name)
		}
		if actualExpanded > maxDOCXExpandedBytes-uint64(written) {
			return nil, errors.New("DOCX archive exceeds the total expanded-size limit")
		}
		actualExpanded += uint64(written)
		actualCompressed += f.CompressedSize64
		if exceedsDOCXRatio(uint64(written), f.CompressedSize64) {
			return nil, fmt.Errorf("DOCX ZIP entry %q exceeds the compression-ratio limit", name)
		}
		if keep {
			return buf.Bytes(), nil
		}
		return nil, nil
	}
	for _, name := range []string{docxContentTypesPath, docxRelationships} {
		f, ok := files[name]
		if !ok || strings.HasSuffix(name, "/") {
			return fmt.Errorf("required DOCX package part %q is missing", name)
		}
		data, err := readMember(name, f, true)
		if err != nil {
			return err
		}
		memberData[name] = data
	}
	mainPart, err := validateDOCXContentTypes(memberData[docxContentTypesPath])
	if err != nil {
		return err
	}
	mainPath, err := validateDOCXRelationships(memberData[docxRelationships])
	if err != nil {
		return err
	}
	if mainPath != mainPart {
		return errors.New("DOCX officeDocument relationship and main content type identify different parts")
	}
	mainFile, ok := files[mainPart]
	if !ok || strings.HasSuffix(mainPart, "/") || mainFile == nil {
		return fmt.Errorf("DOCX main document part %q is missing", mainPart)
	}
	for name, f := range files {
		if name == docxContentTypesPath || name == docxRelationships {
			continue
		}
		data, err := readMember(name, f, name == mainPart)
		if err != nil {
			return err
		}
		if name == mainPart {
			memberData[name] = data
		}
	}
	if actualExpanded != declaredExpanded || actualCompressed != declaredCompressed {
		return errors.New("DOCX ZIP archive has inconsistent size metadata")
	}
	if exceedsDOCXRatio(actualExpanded, actualCompressed) {
		return errors.New("DOCX archive exceeds the overall compression-ratio limit")
	}
	if err := validateDOCXMainDocument(memberData[mainPart]); err != nil {
		return err
	}
	return nil
}

func validateDOCXPath(f *zip.File) (string, bool, error) {
	name := f.Name
	if name == "" || !utf8.ValidString(name) || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") {
		return "", false, fmt.Errorf("unsafe DOCX ZIP path %q", name)
	}
	isDir := strings.HasSuffix(name, "/")
	parts := strings.Split(strings.TrimSuffix(name, "/"), "/")
	if len(parts) == 0 {
		return "", false, fmt.Errorf("unsafe DOCX ZIP path %q", name)
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", false, fmt.Errorf("unsafe DOCX ZIP path %q", name)
		}
	}
	// archive/zip.File.Mode infers directories from a terminal slash, which
	// hides inconsistent Unix mode metadata and can hide a symlink. Inspect
	// the raw Unix type before relying on that inference.
	if f.CreatorVersion>>8 == docxUnixCreator {
		rawType := (f.ExternalAttrs >> 16) & docxUnixTypeMask
		if isDir {
			if rawType != docxUnixDirectory {
				return "", false, fmt.Errorf("DOCX ZIP path %q has inconsistent directory metadata", name)
			}
		} else if rawType != 0 && rawType != docxUnixRegular {
			return "", false, fmt.Errorf("DOCX ZIP path %q has unsupported file type metadata", name)
		}
	}
	mode := f.Mode()
	if isDir != mode.IsDir() || (mode.Type() != 0 && !mode.IsDir()) {
		return "", false, fmt.Errorf("DOCX ZIP path %q has inconsistent file type metadata", name)
	}
	return name, isDir, nil
}

func validateDOCXFlags(f *zip.File) error {
	flags := f.Flags
	allowed := uint16(1<<3 | 1<<11)
	if f.Method == zip.Deflate {
		allowed |= 1<<1 | 1<<2
	}
	if flags&^allowed != 0 {
		return fmt.Errorf("unsupported or encrypted DOCX ZIP flags on %q", f.Name)
	}
	return nil
}

func exceedsDOCXRatio(expanded, compressed uint64) bool {
	if expanded == 0 {
		return false
	}
	if compressed == 0 {
		return true
	}
	q, rem := expanded/compressed, expanded%compressed
	return q > maxDOCXRatio || (q == maxDOCXRatio && rem != 0)
}

type docxOverride struct {
	PartName    string `xml:"PartName,attr"`
	ContentType string `xml:"ContentType,attr"`
}

type docxTypesXML struct {
	XMLName   xml.Name       `xml:"Types"`
	Overrides []docxOverride `xml:"Override"`
}

func validateDOCXContentTypes(data []byte) (string, error) {
	var doc docxTypesXML
	if len(data) == 0 || xml.Unmarshal(data, &doc) != nil || doc.XMLName.Space != docxContentNS {
		return "", errors.New("DOCX content types part is missing or malformed")
	}
	main := ""
	seen := make(map[string]bool, len(doc.Overrides))
	for _, override := range doc.Overrides {
		name, err := cleanDOCXPartName(override.PartName)
		if err != nil || seen[name] {
			return "", errors.New("DOCX content types contain an unsafe or duplicate part name")
		}
		seen[name] = true
		if strings.Contains(strings.ToLower(override.ContentType), "macroenabled") {
			return "", errors.New("macro-enabled DOCX content types are not accepted")
		}
		if override.ContentType == docxMainContentType {
			if main != "" {
				return "", errors.New("DOCX content types identify multiple main document parts")
			}
			main = name
		}
	}
	if main == "" {
		return "", errors.New("DOCX main document content type is missing")
	}
	return main, nil
}

func cleanDOCXPartName(name string) (string, error) {
	if !strings.HasPrefix(name, "/") || strings.HasPrefix(name, "//") || strings.Contains(name, "\\") {
		return "", errors.New("invalid package part name")
	}
	clean := strings.TrimPrefix(name, "/")
	if clean == "" || path.Clean(clean) != clean {
		return "", errors.New("invalid package part name")
	}
	for _, segment := range strings.Split(clean, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", errors.New("invalid package part name")
		}
	}
	return clean, nil
}

type docxRelationship struct {
	ID         string `xml:"Id,attr"`
	Type       string `xml:"Type,attr"`
	Target     string `xml:"Target,attr"`
	TargetMode string `xml:"TargetMode,attr"`
}

type docxRelationshipsXML struct {
	XMLName       xml.Name           `xml:"Relationships"`
	Relationships []docxRelationship `xml:"Relationship"`
}

func validateDOCXRelationships(data []byte) (string, error) {
	var doc docxRelationshipsXML
	if len(data) == 0 || xml.Unmarshal(data, &doc) != nil || doc.XMLName.Space != docxRelationshipsNS {
		return "", errors.New("DOCX root relationships part is missing or malformed")
	}
	main := ""
	for _, rel := range doc.Relationships {
		if rel.Type != docxOfficeRelType {
			continue
		}
		if main != "" || rel.TargetMode != "" && !strings.EqualFold(rel.TargetMode, "Internal") {
			return "", errors.New("DOCX officeDocument relationship is ambiguous or external")
		}
		resolved, err := resolveDOCXRelationshipTarget(rel.Target)
		if err != nil {
			return "", errors.New("DOCX officeDocument relationship has an unsafe target")
		}
		main = resolved
	}
	if main == "" {
		return "", errors.New("DOCX officeDocument relationship is missing")
	}
	return main, nil
}

func resolveDOCXRelationshipTarget(target string) (string, error) {
	if target == "" || strings.Contains(target, "\\") {
		return "", errors.New("invalid relationship target")
	}
	u, err := url.Parse(target)
	if err != nil || u.IsAbs() || u.Host != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("invalid relationship target")
	}
	decoded, err := url.PathUnescape(u.EscapedPath())
	if err != nil || decoded == "" || strings.Contains(decoded, "\\") {
		return "", errors.New("invalid relationship target")
	}
	if strings.HasPrefix(decoded, "/") {
		return cleanDOCXPartName(decoded)
	}
	clean := path.Clean(decoded)
	if clean != decoded || clean == "." || strings.HasPrefix(clean, "../") {
		return "", errors.New("invalid relationship target")
	}
	return clean, nil
}

func validateDOCXMainDocument(data []byte) error {
	type documentRoot struct {
		XMLName xml.Name
	}
	var doc documentRoot
	if len(data) == 0 || xml.Unmarshal(data, &doc) != nil || doc.XMLName.Local != "document" || doc.XMLName.Space != docxWordNS {
		return errors.New("DOCX main document part is missing or malformed")
	}
	return nil
}

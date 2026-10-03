package upload

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"net/url"
	"path"
	"sort"
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
	if err := preflightDOCXZIP(data); err != nil {
		return err
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
	for name := range files {
		parts := strings.Split(strings.TrimSuffix(name, "/"), "/")
		for i := 1; i < len(parts); i++ {
			ancestor := strings.Join(parts[:i], "/")
			if f, ok := files[ancestor]; ok && !strings.HasSuffix(f.Name, "/") {
				return fmt.Errorf("DOCX ZIP file %q conflicts with child path %q", ancestor, name)
			}
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
		checksum := crc32.NewIEEE()
		written, copyErr := io.Copy(dst, io.TeeReader(limited, checksum))
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
		if checksum.Sum32() != f.CRC32 {
			return nil, fmt.Errorf("DOCX ZIP entry %q has an invalid CRC-32 checksum", name)
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
		isRelationships := strings.HasSuffix(strings.ToLower(name), ".rels")
		data, err := readMember(name, f, name == mainPart || isRelationships)
		if err != nil {
			return err
		}
		if isRelationships {
			if err := rejectDOCXMacroRelationships(name, data, files); err != nil {
				return err
			}
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

// preflightDOCXZIP checks archive boundaries before archive/zip can allocate a
// File for every central-directory record. ZIP64 containers and entries are
// deliberately unsupported; their EOCD metadata is still parsed enough to
// enforce the entry ceiling before returning that error.
func preflightDOCXZIP(data []byte) error {
	const (
		eocdSignature       = uint32(0x06054b50)
		zip64LocatorSig     = uint32(0x07064b50)
		zip64EndSig         = uint32(0x06064b50)
		centralSignature    = uint32(0x02014b50)
		localSignature      = uint32(0x04034b50)
		descriptorSignature = uint32(0x08074b50)
	)
	if len(data) < 22 {
		return errors.New("DOCX ZIP end record is truncated")
	}
	eocd := -1
	searchStart := len(data) - (22 + 65535)
	if searchStart < 0 {
		searchStart = 0
	}
	for i := len(data) - 22; i >= searchStart; i-- {
		if binary.LittleEndian.Uint32(data[i:]) != eocdSignature {
			continue
		}
		commentLen := int(binary.LittleEndian.Uint16(data[i+20:]))
		if i+22+commentLen == len(data) {
			eocd = i
			break
		}
	}
	if eocd < 0 {
		return errors.New("DOCX ZIP end record is missing or malformed")
	}
	e := data[eocd:]
	disk := binary.LittleEndian.Uint16(e[4:])
	centralDisk := binary.LittleEndian.Uint16(e[6:])
	diskCount := binary.LittleEndian.Uint16(e[8:])
	count16 := binary.LittleEndian.Uint16(e[10:])
	centralSize32 := binary.LittleEndian.Uint32(e[12:])
	centralOffset32 := binary.LittleEndian.Uint32(e[16:])
	count := uint64(count16)
	centralSize := uint64(centralSize32)
	centralOffset := uint64(centralOffset32)
	hasZIP64Locator := eocd >= 20 && binary.LittleEndian.Uint32(data[eocd-20:]) == zip64LocatorSig
	zip64 := hasZIP64Locator || diskCount == 0xffff || count16 == 0xffff || centralSize32 == 0xffffffff || centralOffset32 == 0xffffffff
	if disk != 0 || centralDisk != 0 || (diskCount != count16 && !zip64) {
		return errors.New("multi-disk DOCX ZIP archives are not supported")
	}
	if zip64 {
		if eocd < 20 || binary.LittleEndian.Uint32(data[eocd-20:]) != zip64LocatorSig {
			return errors.New("DOCX ZIP64 end metadata is missing")
		}
		loc := data[eocd-20 : eocd]
		if binary.LittleEndian.Uint32(loc[4:]) != 0 || binary.LittleEndian.Uint32(loc[16:]) != 1 {
			return errors.New("multi-disk DOCX ZIP64 archives are not supported")
		}
		zoff := binary.LittleEndian.Uint64(loc[8:])
		if zoff > uint64(eocd-20) || zoff+56 > uint64(eocd-20) || zoff > uint64(len(data)) {
			return errors.New("DOCX ZIP64 end record is out of bounds")
		}
		z := data[int(zoff):]
		zsize := binary.LittleEndian.Uint64(z[4:])
		if binary.LittleEndian.Uint32(z) != zip64EndSig || zsize < 44 || zoff+12 > uint64(eocd-20) || zsize != uint64(eocd-20)-zoff-12 {
			return errors.New("DOCX ZIP64 end record is malformed")
		}
		if binary.LittleEndian.Uint32(z[16:]) != 0 || binary.LittleEndian.Uint32(z[20:]) != 0 {
			return errors.New("multi-disk DOCX ZIP64 archives are not supported")
		}
		diskCount64 := binary.LittleEndian.Uint64(z[24:])
		count = binary.LittleEndian.Uint64(z[32:])
		totalCount64 := binary.LittleEndian.Uint64(z[40:])
		if diskCount64 != totalCount64 {
			return errors.New("multi-disk DOCX ZIP64 archives are not supported")
		}
		if count > maxDOCXEntries || totalCount64 > maxDOCXEntries {
			return fmt.Errorf("DOCX ZIP entry count exceeds %d", maxDOCXEntries)
		}
		return errors.New("ZIP64 DOCX archives are not supported")
	}
	if count == 0 || count > maxDOCXEntries {
		return fmt.Errorf("DOCX ZIP entry count must be between 1 and %d", maxDOCXEntries)
	}
	if centralOffset > uint64(eocd) || centralSize > uint64(eocd)-centralOffset || centralOffset+centralSize != uint64(eocd) {
		return errors.New("DOCX ZIP central directory is out of bounds")
	}
	centralEnd := centralOffset + centralSize
	pos := centralOffset
	ranges := make([]docxZIPRange, 0, int(count))
	for i := uint64(0); i < count; i++ {
		if pos > centralEnd || centralEnd-pos < 46 || binary.LittleEndian.Uint32(data[int(pos):]) != centralSignature {
			return errors.New("DOCX ZIP central directory record is malformed")
		}
		h := data[int(pos):]
		nameLen := uint64(binary.LittleEndian.Uint16(h[28:]))
		extraLen := uint64(binary.LittleEndian.Uint16(h[30:]))
		commentLen := uint64(binary.LittleEndian.Uint16(h[32:]))
		recordLen := uint64(46) + nameLen + extraLen + commentLen
		if recordLen > centralEnd-pos {
			return errors.New("DOCX ZIP central directory record exceeds its bounds")
		}
		if binary.LittleEndian.Uint32(h[20:]) == 0xffffffff || binary.LittleEndian.Uint32(h[24:]) == 0xffffffff || binary.LittleEndian.Uint32(h[42:]) == 0xffffffff {
			return errors.New("ZIP64 DOCX entries are not supported")
		}
		if hasDOCXZIP64Extra(h[46+int(nameLen) : 46+int(nameLen+extraLen)]) {
			return errors.New("ZIP64 DOCX entries are not supported")
		}
		r := docxZIPRecord{
			name:        string(h[46 : 46+int(nameLen)]),
			flags:       binary.LittleEndian.Uint16(h[8:]),
			method:      binary.LittleEndian.Uint16(h[10:]),
			crc:         binary.LittleEndian.Uint32(h[16:]),
			compressed:  binary.LittleEndian.Uint32(h[20:]),
			expanded:    binary.LittleEndian.Uint32(h[24:]),
			localOffset: uint64(binary.LittleEndian.Uint32(h[42:])),
		}
		if strings.HasSuffix(r.name, "/") && (r.crc != 0 || r.compressed != 0 || r.expanded != 0) {
			return errors.New("DOCX directory entry contains data")
		}
		end, err := docxLocalRecordEnd(data, r, centralOffset, localSignature, descriptorSignature)
		if err != nil {
			return err
		}
		ranges = append(ranges, docxZIPRange{start: r.localOffset, end: end})
		pos += recordLen
	}
	if pos != centralEnd {
		return errors.New("DOCX ZIP central-directory count or size is inconsistent")
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })
	for i := 1; i < len(ranges); i++ {
		if ranges[i].start < ranges[i-1].end {
			return errors.New("DOCX ZIP member data ranges overlap")
		}
	}
	return nil
}

type docxZIPRecord struct {
	name                      string
	flags, method             uint16
	crc, compressed, expanded uint32
	localOffset               uint64
}

type docxZIPRange struct{ start, end uint64 }

func hasDOCXZIP64Extra(extra []byte) bool {
	for len(extra) >= 4 {
		id := binary.LittleEndian.Uint16(extra)
		n := int(binary.LittleEndian.Uint16(extra[2:]))
		if n > len(extra)-4 {
			return true
		}
		if id == 1 {
			return true
		}
		extra = extra[4+n:]
	}
	return len(extra) != 0
}

func docxLocalRecordEnd(data []byte, r docxZIPRecord, centralOffset uint64, localSignature, descriptorSignature uint32) (uint64, error) {
	bad := func() (uint64, error) {
		return 0, fmt.Errorf("DOCX ZIP local header for %q is malformed or inconsistent", r.name)
	}
	if r.localOffset > centralOffset || centralOffset-r.localOffset < 30 || r.localOffset > uint64(len(data)) {
		return bad()
	}
	h := data[int(r.localOffset):]
	if binary.LittleEndian.Uint32(h) != localSignature {
		return bad()
	}
	flags := binary.LittleEndian.Uint16(h[6:])
	method := binary.LittleEndian.Uint16(h[8:])
	nameLen := uint64(binary.LittleEndian.Uint16(h[26:]))
	extraLen := uint64(binary.LittleEndian.Uint16(h[28:]))
	headerLen := uint64(30) + nameLen + extraLen
	if headerLen > centralOffset-r.localOffset {
		return bad()
	}
	if flags != r.flags || method != r.method || string(h[30:30+int(nameLen)]) != r.name {
		return bad()
	}
	if hasDOCXZIP64Extra(h[30+int(nameLen) : int(headerLen)]) {
		return 0, errors.New("ZIP64 DOCX entries are not supported")
	}
	lcrc := binary.LittleEndian.Uint32(h[14:])
	lcomp := binary.LittleEndian.Uint32(h[18:])
	lexp := binary.LittleEndian.Uint32(h[22:])
	if lcomp == 0xffffffff || lexp == 0xffffffff {
		return 0, errors.New("ZIP64 DOCX entries are not supported")
	}
	if flags&(1<<3) == 0 {
		if lcrc != r.crc || lcomp != r.compressed || lexp != r.expanded {
			return bad()
		}
	} else if (lcrc != 0 && lcrc != r.crc) || (lcomp != 0 && lcomp != r.compressed) || (lexp != 0 && lexp != r.expanded) {
		return bad()
	}
	dataStart := r.localOffset + headerLen
	if uint64(r.compressed) > centralOffset-dataStart {
		return bad()
	}
	end := dataStart + uint64(r.compressed)
	if flags&(1<<3) != 0 {
		if end > centralOffset || centralOffset-end < 12 {
			return bad()
		}
		d := data[int(end):]
		descriptorLen := uint64(12)
		if len(d) >= 16 && binary.LittleEndian.Uint32(d) == descriptorSignature && binary.LittleEndian.Uint32(d[4:]) == r.crc && binary.LittleEndian.Uint32(d[8:]) == r.compressed && binary.LittleEndian.Uint32(d[12:]) == r.expanded {
			descriptorLen = 16
			d = d[4:]
		}
		if len(d) < 12 || binary.LittleEndian.Uint32(d) != r.crc || binary.LittleEndian.Uint32(d[4:]) != r.compressed || binary.LittleEndian.Uint32(d[8:]) != r.expanded {
			return bad()
		}
		end += descriptorLen
	}
	if end > centralOffset {
		return bad()
	}
	return end, nil
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

type docxDefault struct {
	Extension   string `xml:"Extension,attr"`
	ContentType string `xml:"ContentType,attr"`
}

type docxTypesXML struct {
	XMLName   xml.Name       `xml:"Types"`
	Defaults  []docxDefault  `xml:"Default"`
	Overrides []docxOverride `xml:"Override"`
}

func validateDOCXContentTypes(data []byte) (string, error) {
	var doc docxTypesXML
	if len(data) == 0 || xml.Unmarshal(data, &doc) != nil || doc.XMLName.Space != docxContentNS {
		return "", errors.New("DOCX content types part is missing or malformed")
	}
	main := ""
	for _, def := range doc.Defaults {
		if def.Extension == "" || isDOCXMacroMetadata(def.ContentType) {
			return "", errors.New("macro-bearing DOCX content types are not accepted")
		}
	}
	seen := make(map[string]bool, len(doc.Overrides))
	for _, override := range doc.Overrides {
		name, err := cleanDOCXPartName(override.PartName)
		if err != nil || seen[name] {
			return "", errors.New("DOCX content types contain an unsafe or duplicate part name")
		}
		seen[name] = true
		if isDOCXMacroMetadata(override.ContentType) {
			return "", errors.New("macro-bearing DOCX content types are not accepted")
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

func isDOCXMacroMetadata(value string) bool {
	v := strings.ToLower(value)
	return strings.Contains(v, "macroenabled") || strings.Contains(v, "vbaproject") || strings.Contains(v, "vbadata") || strings.Contains(v, "macrosheet") || strings.Contains(v, "dialogsheet")
}

func rejectDOCXMacroRelationships(relPart string, data []byte, files map[string]*zip.File) error {
	var doc docxRelationshipsXML
	if len(data) == 0 || xml.Unmarshal(data, &doc) != nil || doc.XMLName.Space != docxRelationshipsNS {
		return fmt.Errorf("DOCX relationships part %q is malformed", relPart)
	}
	ownerDir := path.Dir(relPart)
	if strings.Contains(ownerDir, "_rels") {
		ownerDir = path.Dir(ownerDir)
	}
	for _, rel := range doc.Relationships {
		kind := strings.ToLower(rel.Type)
		if strings.Contains(kind, "vbaproject") || strings.Contains(kind, "vbadata") || strings.Contains(kind, "macrosheet") || strings.Contains(kind, "dialogsheet") {
			target, err := resolveDOCXRelationshipTarget(rel.Target)
			if err != nil {
				return errors.New("DOCX macro relationship has an unsafe target")
			}
			if !strings.HasPrefix(rel.Target, "/") {
				target = path.Clean(path.Join(ownerDir, target))
			}
			if _, ok := files[target]; !ok {
				return errors.New("DOCX macro relationship target is missing")
			}
			return errors.New("macro-bearing DOCX relationships are not accepted")
		}
	}
	return nil
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

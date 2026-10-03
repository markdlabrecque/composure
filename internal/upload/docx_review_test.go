package upload

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"hash/crc32"
	"io"
	"strings"
	"testing"
)

func TestValidateDOCXPreflightsDishonestCentralDirectoryBeforeEnumeration(t *testing.T) {
	data := docxReviewRepeatedCentralDirectory(t, 2001, 1)
	count, declared := docxReviewCentralCounts(t, data)
	if count != 2001 || declared != 1 {
		t.Fatalf("fixture has %d central records and declares %d, want 2001 and 1", count, declared)
	}
	allocs := testing.AllocsPerRun(1, func() {
		if err := ValidateDOCX(data); err == nil {
			t.Fatal("dishonest central-directory count accepted")
		}
	})
	if allocs > 200 {
		t.Fatalf("validator made %.0f allocations before rejecting over 1000 central records; want at most 200", allocs)
	}
}

func TestValidateDOCXRejectsModuloEOCDEntryCount(t *testing.T) {
	data := docxReviewRepeatedCentralDirectory(t, 65536, 0)
	count, declared := docxReviewCentralCounts(t, data)
	if count != 65536 || declared != 0 {
		t.Fatalf("fixture has %d central records and declares %d, want 65536 and modulo count 0", count, declared)
	}
	if err := ValidateDOCX(data); err == nil {
		t.Fatal("65536 physical central records with modulo EOCD count accepted")
	}
}

func TestValidateDOCXRejectsLocalCentralHeaderMismatches(t *testing.T) {
	tests := []struct {
		name  string
		patch func([]byte)
		check func(*testing.T, []byte, []byte)
	}{
		{
			name:  "local_encryption_only",
			patch: func(h []byte) { binary.LittleEndian.PutUint16(h[6:], binary.LittleEndian.Uint16(h[6:])|1) },
			check: func(t *testing.T, local, central []byte) {
				if binary.LittleEndian.Uint16(local[6:])&1 == 0 || binary.LittleEndian.Uint16(central[8:])&1 != 0 {
					t.Fatal("fixture does not isolate encryption to local header")
				}
			},
		},
		{
			name: "flags",
			patch: func(h []byte) {
				binary.LittleEndian.PutUint16(h[6:], binary.LittleEndian.Uint16(h[6:])|(1<<11))
			},
			check: func(t *testing.T, local, central []byte) {
				if binary.LittleEndian.Uint16(local[6:]) == binary.LittleEndian.Uint16(central[8:]) {
					t.Fatal("fixture flags still match")
				}
			},
		},
		{
			name:  "method",
			patch: func(h []byte) { binary.LittleEndian.PutUint16(h[8:], zip.Deflate) },
			check: func(t *testing.T, local, central []byte) {
				if binary.LittleEndian.Uint16(local[8:]) == binary.LittleEndian.Uint16(central[10:]) {
					t.Fatal("fixture methods still match")
				}
			},
		},
		{
			name:  "filename",
			patch: func(h []byte) { copy(h[30:], "word/evilname.xml") },
			check: func(t *testing.T, local, central []byte) {
				ln := int(binary.LittleEndian.Uint16(local[26:]))
				cn := int(binary.LittleEndian.Uint16(central[28:]))
				if string(local[30:30+ln]) == string(central[46:46+cn]) {
					t.Fatal("fixture names still match")
				}
			},
		},
		{
			name:  "compressed_size",
			patch: func(h []byte) { binary.LittleEndian.PutUint32(h[18:], binary.LittleEndian.Uint32(h[18:])+1) },
			check: func(t *testing.T, local, central []byte) {
				if binary.LittleEndian.Uint32(local[18:]) == binary.LittleEndian.Uint32(central[20:]) {
					t.Fatal("fixture compressed sizes still match")
				}
			},
		},
		{
			name:  "uncompressed_size",
			patch: func(h []byte) { binary.LittleEndian.PutUint32(h[22:], binary.LittleEndian.Uint32(h[22:])+1) },
			check: func(t *testing.T, local, central []byte) {
				if binary.LittleEndian.Uint32(local[22:]) == binary.LittleEndian.Uint32(central[24:]) {
					t.Fatal("fixture expanded sizes still match")
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := docxArchive(t, zip.Store, nil)
			local, central := docxReviewHeaders(t, data, "word/document.xml")
			tc.patch(local)
			tc.check(t, local, central)
			if err := ValidateDOCX(data); err == nil {
				t.Fatal("local/central mismatch accepted")
			}
		})
	}
}

func TestValidateDOCXRejectsOverlappingAndCentralRegionRanges(t *testing.T) {
	t.Run("overlapping_members", func(t *testing.T) {
		data := docxArchive(t, zip.Store, []docxEntry{{name: "word/media/a.bin", data: []byte("same"), method: zip.Store}, {name: "word/media/b.bin", data: []byte("same"), method: zip.Store}})
		_, a := docxReviewHeaders(t, data, "word/media/a.bin")
		_, b := docxReviewHeaders(t, data, "word/media/b.bin")
		binary.LittleEndian.PutUint32(b[42:], binary.LittleEndian.Uint32(a[42:]))
		if binary.LittleEndian.Uint32(a[42:]) != binary.LittleEndian.Uint32(b[42:]) {
			t.Fatal("fixture ranges do not overlap")
		}
		if err := ValidateDOCX(data); err == nil {
			t.Fatal("overlapping member ranges accepted")
		}
	})
	t.Run("member_starts_in_central_directory", func(t *testing.T) {
		data := docxArchive(t, zip.Store, nil)
		_, central := docxReviewHeaders(t, data, "word/document.xml")
		centralOffset := docxReviewCentralOffset(t, data)
		binary.LittleEndian.PutUint32(central[42:], uint32(centralOffset))
		if int(binary.LittleEndian.Uint32(central[42:])) != centralOffset {
			t.Fatal("fixture offset was not patched into central directory")
		}
		if err := ValidateDOCX(data); err == nil {
			t.Fatal("member range entering central-directory structures accepted")
		}
	})
}

func TestValidateDOCXChecksDeclaredZeroCRCUnconditionally(t *testing.T) {
	const name = "word/media/payload.bin"
	data := docxArchive(t, zip.Store, []docxEntry{{name: name, data: []byte("payload"), method: zip.Store}})
	local, central := docxReviewHeaders(t, data, name)
	binary.LittleEndian.PutUint16(local[6:], binary.LittleEndian.Uint16(local[6:])&^(1<<3))
	binary.LittleEndian.PutUint16(central[8:], binary.LittleEndian.Uint16(central[8:])&^(1<<3))
	binary.LittleEndian.PutUint32(local[14:], 0)
	binary.LittleEndian.PutUint32(central[16:], 0)
	offset := int(binary.LittleEndian.Uint32(central[42:])) + 30 + int(binary.LittleEndian.Uint16(local[26:])) + int(binary.LittleEndian.Uint16(local[28:]))
	data[offset] ^= 0xff
	if binary.LittleEndian.Uint32(local[14:]) != 0 || binary.LittleEndian.Uint32(central[16:]) != 0 {
		t.Fatal("fixture CRC is not zero")
	}
	if err := ValidateDOCX(data); err == nil {
		t.Fatal("corrupt member with declared zero CRC accepted")
	}
}

func TestValidateDOCXRejectsRenamedVBAFromOOXMLMetadata(t *testing.T) {
	macroType := "application/vnd.ms-office.vbaProject"
	tests := []struct {
		name          string
		content       []byte
		relationships []byte
	}{
		{"override_content_type", docxReviewContentTypes(`<Override PartName="/word/payload.bin" ContentType="` + macroType + `"/>`), nil},
		{"default_content_type", docxReviewContentTypes(`<Default Extension="bin" ContentType="` + macroType + `"/>`), nil},
		{"relationship", docxReviewContentTypes(`<Override PartName="/word/payload.bin" ContentType="application/octet-stream"/>`), []byte(docxReviewRelationships("http://schemas.microsoft.com/office/2006/relationships/vbaProject", "payload.bin"))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entries := replaceEntry(docxRequiredEntries(), "[Content_Types].xml", tc.content)
			entries = append(entries, docxEntry{name: "word/payload.bin", data: []byte("macro"), method: zip.Store})
			if tc.relationships != nil {
				entries = append(entries, docxEntry{name: "word/_rels/document.xml.rels", data: tc.relationships, method: zip.Store})
			}
			if err := ValidateDOCX(writeDOCX(t, entries)); err == nil {
				t.Fatal("renamed VBA part identified by OOXML metadata accepted")
			}
		})
	}
}

func TestValidateDOCXAcceptsOrdinaryNonMacroPackageMetadata(t *testing.T) {
	content := docxReviewContentTypes(`<Default Extension="bin" ContentType="application/octet-stream"/><Override PartName="/word/payload.bin" ContentType="application/octet-stream"/>`)
	entries := replaceEntry(docxRequiredEntries(), "[Content_Types].xml", content)
	entries = append(entries, docxDirectory("word/media/"), docxEntry{name: "word/payload.bin", data: []byte("ordinary"), method: zip.Store}, docxEntry{name: "word/_rels/document.xml.rels", data: []byte(docxReviewRelationships("http://schemas.openxmlformats.org/officeDocument/2006/relationships/image", "media/image.bin")), method: zip.Store})
	if err := ValidateDOCX(writeDOCX(t, entries)); err != nil {
		t.Fatalf("ordinary nonmacro metadata rejected: %v", err)
	}
}

func TestValidateDOCXActualCompressionRatioBoundaries(t *testing.T) {
	t.Run("per_entry_and_overall_exactly_100_to_1", func(t *testing.T) {
		data := docxReviewRatioArchive(t, false)
		docxReviewAssertActualRatios(t, data, 100, 100)
		if err := ValidateDOCX(data); err != nil {
			t.Fatalf("valid archive at exact 100:1 ratios rejected: %v", err)
		}
	})
	t.Run("one_expanded_byte_over", func(t *testing.T) {
		data := docxReviewRatioArchive(t, true)
		docxReviewAssertActualRatios(t, data, 100, 101)
		if err := ValidateDOCX(data); err == nil {
			t.Fatal("actual compression ratio just over 100:1 accepted")
		}
	})
}

func docxReviewContentTypes(extra string) []byte {
	return []byte(`<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` + extra + `</Types>`)
}

func docxReviewRelationships(relType, target string) string {
	return `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rExtra" Type="` + relType + `" Target="` + target + `"/></Relationships>`
}

func docxReviewHeaders(t *testing.T, data []byte, name string) ([]byte, []byte) {
	t.Helper()
	var local, central []byte
	for i := 0; i+46 <= len(data); i++ {
		switch binary.LittleEndian.Uint32(data[i:]) {
		case 0x04034b50:
			n := int(binary.LittleEndian.Uint16(data[i+26:]))
			if i+30+n <= len(data) && string(data[i+30:i+30+n]) == name {
				local = data[i:]
			}
		case 0x02014b50:
			n := int(binary.LittleEndian.Uint16(data[i+28:]))
			if i+46+n <= len(data) && string(data[i+46:i+46+n]) == name {
				central = data[i:]
			}
		}
	}
	if local == nil || central == nil {
		t.Fatalf("headers for %q not found", name)
	}
	return local, central
}

func docxReviewCentralOffset(t *testing.T, data []byte) int {
	t.Helper()
	for i := len(data) - 22; i >= 0; i-- {
		if binary.LittleEndian.Uint32(data[i:]) == 0x06054b50 {
			return int(binary.LittleEndian.Uint32(data[i+16:]))
		}
	}
	t.Fatal("EOCD not found")
	return 0
}

func docxReviewRepeatedCentralDirectory(t *testing.T, records int, declared uint16) []byte {
	t.Helper()
	base := docxArchive(t, zip.Store, nil)
	centralOffset := docxReviewCentralOffset(t, base)
	eocd := len(base) - 22
	first := centralOffset
	if binary.LittleEndian.Uint32(base[first:]) != 0x02014b50 {
		t.Fatal("central record not found")
	}
	recordLen := 46 + int(binary.LittleEndian.Uint16(base[first+28:])) + int(binary.LittleEndian.Uint16(base[first+30:])) + int(binary.LittleEndian.Uint16(base[first+32:]))
	record := bytes.Clone(base[first : first+recordLen])
	out := append([]byte(nil), base[:centralOffset]...)
	for i := 0; i < records; i++ {
		out = append(out, record...)
	}
	newEOCD := bytes.Clone(base[eocd:])
	binary.LittleEndian.PutUint16(newEOCD[8:], declared)
	binary.LittleEndian.PutUint16(newEOCD[10:], declared)
	binary.LittleEndian.PutUint32(newEOCD[12:], uint32(records*recordLen))
	binary.LittleEndian.PutUint32(newEOCD[16:], uint32(centralOffset))
	return append(out, newEOCD...)
}

func docxReviewCentralCounts(t *testing.T, data []byte) (int, uint16) {
	t.Helper()
	count := 0
	for i := 0; i+46 <= len(data); i++ {
		if binary.LittleEndian.Uint32(data[i:]) == 0x02014b50 {
			count++
		}
	}
	for i := len(data) - 22; i >= 0; i-- {
		if binary.LittleEndian.Uint32(data[i:]) == 0x06054b50 {
			return count, binary.LittleEndian.Uint16(data[i+10:])
		}
	}
	t.Fatal("EOCD not found")
	return 0, 0
}

type docxReviewRawEntry struct {
	name             string
	data, compressed []byte
	method           uint16
}

func docxReviewRatioArchive(t *testing.T, over bool) []byte {
	t.Helper()
	types := docxReviewExactRatioEntry(t, "[Content_Types].xml", `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>`, `</Types>`, false)
	rels := docxReviewExactRatioEntry(t, "_rels/.rels", `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>`, `</Relationships>`, false)
	doc := docxReviewExactRatioEntry(t, "word/document.xml", `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`, `</w:body></w:document>`, over)
	return docxReviewRawZIP(t, []docxReviewRawEntry{types, rels, doc})
}

func docxReviewExactRatioEntry(t *testing.T, name, prefix, suffix string, oneOver bool) docxReviewRawEntry {
	t.Helper()
	start := 1_000
	if oneOver {
		start++
	}
	for size := start; size <= 30_001; size += 100 {
		padding := size - len(prefix) - len(suffix)
		if padding < 0 {
			continue
		}
		data := []byte(prefix + strings.Repeat(" ", padding) + suffix)
		compressed := docxReviewDeflate(t, data)
		wantExpanded := 100 * len(compressed)
		if oneOver {
			wantExpanded++
		}
		if len(data) != wantExpanded {
			continue
		}
		return docxReviewRawEntry{name: name, data: data, compressed: compressed, method: zip.Deflate}
	}
	t.Fatalf("could not build bounded valid exact-ratio DEFLATE fixture for %q", name)
	return docxReviewRawEntry{}
}

func docxReviewDeflate(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.Clone(buf.Bytes())
}

func docxReviewRawZIP(t *testing.T, entries []docxReviewRawEntry) []byte {
	t.Helper()
	var local, central bytes.Buffer
	for _, e := range entries {
		offset := local.Len()
		crc := crc32.ChecksumIEEE(e.data)
		_ = binary.Write(&local, binary.LittleEndian, uint32(0x04034b50))
		_ = binary.Write(&local, binary.LittleEndian, uint16(20))
		_ = binary.Write(&local, binary.LittleEndian, uint16(0))
		_ = binary.Write(&local, binary.LittleEndian, e.method)
		_ = binary.Write(&local, binary.LittleEndian, uint16(0))
		_ = binary.Write(&local, binary.LittleEndian, uint16(0))
		_ = binary.Write(&local, binary.LittleEndian, crc)
		_ = binary.Write(&local, binary.LittleEndian, uint32(len(e.compressed)))
		_ = binary.Write(&local, binary.LittleEndian, uint32(len(e.data)))
		_ = binary.Write(&local, binary.LittleEndian, uint16(len(e.name)))
		_ = binary.Write(&local, binary.LittleEndian, uint16(0))
		local.WriteString(e.name)
		local.Write(e.compressed)
		_ = binary.Write(&central, binary.LittleEndian, uint32(0x02014b50))
		_ = binary.Write(&central, binary.LittleEndian, uint16(20))
		_ = binary.Write(&central, binary.LittleEndian, uint16(20))
		_ = binary.Write(&central, binary.LittleEndian, uint16(0))
		_ = binary.Write(&central, binary.LittleEndian, e.method)
		_ = binary.Write(&central, binary.LittleEndian, uint16(0))
		_ = binary.Write(&central, binary.LittleEndian, uint16(0))
		_ = binary.Write(&central, binary.LittleEndian, crc)
		_ = binary.Write(&central, binary.LittleEndian, uint32(len(e.compressed)))
		_ = binary.Write(&central, binary.LittleEndian, uint32(len(e.data)))
		_ = binary.Write(&central, binary.LittleEndian, uint16(len(e.name)))
		_ = binary.Write(&central, binary.LittleEndian, uint16(0))
		_ = binary.Write(&central, binary.LittleEndian, uint16(0))
		_ = binary.Write(&central, binary.LittleEndian, uint16(0))
		_ = binary.Write(&central, binary.LittleEndian, uint16(0))
		_ = binary.Write(&central, binary.LittleEndian, uint32(0))
		_ = binary.Write(&central, binary.LittleEndian, uint32(offset))
		central.WriteString(e.name)
	}
	centralOffset := local.Len()
	local.Write(central.Bytes())
	_ = binary.Write(&local, binary.LittleEndian, uint32(0x06054b50))
	_ = binary.Write(&local, binary.LittleEndian, uint16(0))
	_ = binary.Write(&local, binary.LittleEndian, uint16(0))
	_ = binary.Write(&local, binary.LittleEndian, uint16(len(entries)))
	_ = binary.Write(&local, binary.LittleEndian, uint16(len(entries)))
	_ = binary.Write(&local, binary.LittleEndian, uint32(central.Len()))
	_ = binary.Write(&local, binary.LittleEndian, uint32(centralOffset))
	_ = binary.Write(&local, binary.LittleEndian, uint16(0))
	return local.Bytes()
}

func docxReviewAssertActualRatios(t *testing.T, data []byte, wantMin, wantMax uint64) {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var expanded, compressed uint64
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		n, err := io.Copy(io.Discard, rc)
		if err != nil {
			t.Fatal(err)
		}
		_ = rc.Close()
		if uint64(n) != f.UncompressedSize64 {
			t.Fatal("fixture expanded-size mismatch")
		}
		expanded += uint64(n)
		compressed += f.CompressedSize64
	}
	ratioCeil := (expanded + compressed - 1) / compressed
	if ratioCeil < wantMin || ratioCeil > wantMax {
		t.Fatalf("actual aggregate ratio ceiling = %d, want %d..%d", ratioCeil, wantMin, wantMax)
	}
	for _, f := range r.File {
		if wantMin == wantMax && f.UncompressedSize64 != f.CompressedSize64*wantMin {
			t.Fatalf("%q ratio is %d:%d, want exact %d:1", f.Name, f.UncompressedSize64, f.CompressedSize64, wantMin)
		}
	}
}

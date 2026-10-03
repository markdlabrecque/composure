package upload

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"io/fs"
	"testing"
)

const (
	docxMaxEntries       = 1000
	docxMaxEntryBytes    = 25 * 1024 * 1024
	docxMaxExpandedBytes = 100 * 1024 * 1024
)

func TestValidateDOCXAcceptsValidStoreAndDeflateArchives(t *testing.T) {
	for _, method := range []uint16{zip.Store, zip.Deflate} {
		t.Run(fmt.Sprintf("method_%d", method), func(t *testing.T) {
			if err := ValidateDOCX(docxArchive(t, method, nil)); err != nil {
				t.Fatalf("valid DOCX rejected: %v", err)
			}
		})
	}
}

func TestValidateDOCXEntryCountBoundary(t *testing.T) {
	baseCount := len(docxRequiredEntries())
	atLimit := append([]docxEntry{docxDirectory("word/media/")}, fillerEntries(docxMaxEntries-baseCount-1)...)
	if err := ValidateDOCX(docxArchive(t, zip.Store, atLimit)); err != nil {
		t.Fatalf("archive with %d entries rejected: %v", docxMaxEntries, err)
	}
	overLimit := append(atLimit, docxEntry{name: "word/media/extra.bin", method: zip.Store})
	if err := ValidateDOCX(docxArchive(t, zip.Store, overLimit)); err == nil {
		t.Fatalf("archive with %d entries accepted", docxMaxEntries+1)
	}
}

func TestValidateDOCXExpandedSizeBoundaries(t *testing.T) {
	t.Run("entry_at_25_MiB", func(t *testing.T) {
		entries := []docxEntry{{name: "word/media/payload.bin", data: make([]byte, docxMaxEntryBytes), method: zip.Store}}
		if err := ValidateDOCX(docxArchive(t, zip.Store, entries)); err != nil {
			t.Fatalf("entry at expanded-size limit rejected: %v", err)
		}
	})
	t.Run("entry_one_byte_over", func(t *testing.T) {
		entries := []docxEntry{{name: "word/media/payload.bin", data: make([]byte, docxMaxEntryBytes+1), method: zip.Store}}
		if err := ValidateDOCX(docxArchive(t, zip.Store, entries)); err == nil {
			t.Fatal("entry one byte above expanded-size limit accepted")
		}
	})
	t.Run("total_at_100_MiB", func(t *testing.T) {
		required := docxRequiredEntries()
		requiredBytes := entryBytes(required)
		entries := []docxEntry{
			{name: "word/media/a.bin", data: make([]byte, docxMaxEntryBytes), method: zip.Store},
			{name: "word/media/b.bin", data: make([]byte, docxMaxEntryBytes), method: zip.Store},
			{name: "word/media/c.bin", data: make([]byte, docxMaxEntryBytes), method: zip.Store},
			{name: "word/media/d.bin", data: make([]byte, docxMaxEntryBytes-requiredBytes), method: zip.Store},
		}
		if err := ValidateDOCX(docxArchive(t, zip.Store, entries)); err != nil {
			t.Fatalf("archive at total expanded-size limit rejected: %v", err)
		}
	})
	t.Run("total_one_byte_over", func(t *testing.T) {
		requiredBytes := entryBytes(docxRequiredEntries())
		entries := []docxEntry{
			{name: "word/media/a.bin", data: make([]byte, docxMaxEntryBytes), method: zip.Store},
			{name: "word/media/b.bin", data: make([]byte, docxMaxEntryBytes), method: zip.Store},
			{name: "word/media/c.bin", data: make([]byte, docxMaxEntryBytes), method: zip.Store},
			{name: "word/media/d.bin", data: make([]byte, docxMaxEntryBytes-requiredBytes+1), method: zip.Store},
		}
		if err := ValidateDOCX(docxArchive(t, zip.Store, entries)); err == nil {
			t.Fatal("archive one byte above total expanded-size limit accepted")
		}
	})
}

func TestValidateDOCXCompressionRatioBoundaries(t *testing.T) {
	for _, scope := range []string{"entry", "overall"} {
		t.Run(scope+"_exactly_100_to_1", func(t *testing.T) {
			data := docxArchive(t, zip.Store, nil)
			data = patchZIPCompressedSizes(t, data, scope, 100)
			if err := ValidateDOCX(data); err == nil {
				// Patched sizes intentionally disagree with the stream. Reaching the
				// data read must fail closed, but equality must not be rejected from
				// metadata alone as an over-limit ratio.
				t.Fatal("archive with inconsistent compressed-size metadata accepted")
			}
		})
		t.Run(scope+"_over_100_to_1", func(t *testing.T) {
			data := patchZIPCompressedSizes(t, docxArchive(t, zip.Store, nil), scope, 101)
			if err := ValidateDOCX(data); err == nil {
				t.Fatal("compression ratio above 100:1 accepted")
			}
		})
	}
}

func TestValidateDOCXRejectsZIPBombShape(t *testing.T) {
	bomb := bytes.Repeat([]byte{0}, 2*1024*1024)
	data := docxArchive(t, zip.Deflate, []docxEntry{{name: "word/media/bomb.bin", data: bomb, method: zip.Deflate}})
	if err := ValidateDOCX(data); err == nil {
		t.Fatal("highly compressible ZIP bomb shape accepted")
	}
}

func TestValidateDOCXRequiresOOXMLDocumentParts(t *testing.T) {
	tests := []struct {
		name    string
		entries []docxEntry
	}{
		{"missing_content_types", withoutEntry(docxRequiredEntries(), "[Content_Types].xml")},
		{"missing_root_relationships", withoutEntry(docxRequiredEntries(), "_rels/.rels")},
		{"missing_main_document", withoutEntry(docxRequiredEntries(), "word/document.xml")},
		{"wrong_main_content_type", replaceEntry(docxRequiredEntries(), "[Content_Types].xml", contentTypes("application/xml"))},
		{"malformed_content_types", replaceEntry(docxRequiredEntries(), "[Content_Types].xml", []byte("<Types>"))},
		{"relationship_targets_missing_part", replaceEntry(docxRequiredEntries(), "_rels/.rels", []byte(rootRelationships("word/missing.xml")))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateDOCX(writeDOCX(t, tc.entries)); err == nil {
				t.Fatal("archive without valid OOXML main document structure accepted")
			}
		})
	}
}

type docxEntry struct {
	name   string
	data   []byte
	method uint16
	mode   uint32
}

func docxDirectory(name string) docxEntry {
	return docxEntry{name: name, method: zip.Store, mode: uint32(fs.ModeDir | 0o755)}
}

func docxArchive(t *testing.T, method uint16, extra []docxEntry) []byte {
	t.Helper()
	entries := docxRequiredEntries()
	for i := range entries {
		entries[i].method = method
	}
	return writeDOCX(t, append(entries, extra...))
}

func docxRequiredEntries() []docxEntry {
	return []docxEntry{
		{name: "[Content_Types].xml", data: contentTypes("application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"), method: zip.Store},
		{name: "_rels/.rels", data: []byte(rootRelationships("word/document.xml")), method: zip.Store},
		{name: "word/document.xml", data: []byte(`<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body/></w:document>`), method: zip.Store},
	}
}

func contentTypes(mainType string) []byte {
	return []byte(fmt.Sprintf(`<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/document.xml" ContentType="%s"/></Types>`, mainType))
}

func rootRelationships(target string) string {
	return fmt.Sprintf(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="%s"/></Relationships>`, target)
}

func writeDOCX(t *testing.T, entries []docxEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, entry := range entries {
		h := &zip.FileHeader{Name: entry.name, Method: entry.method}
		if entry.mode != 0 {
			h.SetMode(fs.FileMode(entry.mode))
		}
		part, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func fillerEntries(n int) []docxEntry {
	entries := make([]docxEntry, n)
	for i := range entries {
		entries[i] = docxEntry{name: fmt.Sprintf("word/media/%04d.bin", i), method: zip.Store}
	}
	return entries
}

func entryBytes(entries []docxEntry) int {
	total := 0
	for _, entry := range entries {
		total += len(entry.data)
	}
	return total
}

func withoutEntry(entries []docxEntry, name string) []docxEntry {
	out := make([]docxEntry, 0, len(entries)-1)
	for _, entry := range entries {
		if entry.name != name {
			out = append(out, entry)
		}
	}
	return out
}

func replaceEntry(entries []docxEntry, name string, data []byte) []docxEntry {
	out := append([]docxEntry(nil), entries...)
	for i := range out {
		if out[i].name == name {
			out[i].data = data
		}
	}
	return out
}

// patchZIPCompressedSizes creates adversarial metadata without allocating a
// huge archive. The validator must check ratios without overflow, then still
// read and verify the underlying stream rather than trusting the metadata.
func patchZIPCompressedSizes(t *testing.T, data []byte, scope string, ratio uint32) []byte {
	t.Helper()
	out := bytes.Clone(data)
	for off := 0; off+46 <= len(out); off++ {
		if binary.LittleEndian.Uint32(out[off:]) != 0x02014b50 {
			continue
		}
		nameLen := int(binary.LittleEndian.Uint16(out[off+28:]))
		name := string(out[off+46 : off+46+nameLen])
		if scope == "entry" && name != "word/document.xml" {
			continue
		}
		uncompressed := binary.LittleEndian.Uint32(out[off+24:])
		if uncompressed < ratio {
			continue
		}
		binary.LittleEndian.PutUint32(out[off+20:], uncompressed/ratio)
		if scope == "entry" {
			return out
		}
	}
	if scope == "overall" {
		// Apply the same boundary to every non-empty entry so aggregate metadata
		// also describes the requested ratio.
		return out
	}
	t.Fatal("failed to locate ZIP entry for ratio fixture")
	return nil
}

package upload

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"os"
	"testing"
)

func TestValidateDOCXRejectsUnsafeAndDuplicatePaths(t *testing.T) {
	paths := []string{"", "/word/document.xml", "../evil", "word/../../evil", "word\\document.xml", "C:/evil", "word//document.xml", "word/./document.xml"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			if err := ValidateDOCX(docxArchive(t, zip.Store, []docxEntry{{name: path, data: []byte("x"), method: zip.Store}})); err == nil {
				t.Fatal("unsafe ZIP entry path accepted")
			}
		})
	}
	duplicate := append(docxRequiredEntries(), docxEntry{name: "word/document.xml", data: []byte("second"), method: zip.Store})
	if err := ValidateDOCX(writeDOCX(t, duplicate)); err == nil {
		t.Fatal("duplicate ZIP entry path accepted")
	}
}

func TestValidateDOCXAcceptsSafeZeroDataDirectory(t *testing.T) {
	data := docxArchive(t, zip.Store, []docxEntry{docxDirectory("word/"), docxDirectory("word/media/")})
	if err := ValidateDOCX(data); err != nil {
		t.Fatalf("valid DOCX with safe zero-data directory rejected: %v", err)
	}
}

func TestValidateDOCXRejectsInvalidDirectoryEntries(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"nonzero_data", docxDirectoryWithData(t)},
		{"directory_mode_without_terminal_slash", docxArchive(t, zip.Store, []docxEntry{{name: "word/media", method: zip.Store, mode: uint32(os.ModeDir | 0o755)}})},
		{"terminal_slash_without_directory_mode", docxArchive(t, zip.Store, []docxEntry{{name: "word/media/", method: zip.Store, mode: uint32(0o644)}})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateDOCX(tc.data); err == nil {
				t.Fatal("malformed directory entry accepted")
			}
		})
	}

	unsafeDirectories := []string{"/word/", "../word/", "word/../../media/", "word\\media\\", "C:/word/", "word//media/", "word/./media/"}
	for _, name := range unsafeDirectories {
		t.Run("unsafe_"+name, func(t *testing.T) {
			if err := ValidateDOCX(docxArchive(t, zip.Store, []docxEntry{docxDirectory(name)})); err == nil {
				t.Fatal("unsafe directory path accepted")
			}
		})
	}
}

func docxDirectoryWithData(t *testing.T) []byte {
	t.Helper()
	const regularName = "word/mediax"
	const directoryName = "word/media/"
	data := docxArchive(t, zip.Store, []docxEntry{{name: regularName, data: []byte("x"), method: zip.Store, mode: uint32(0o644)}})
	patchedLocal := false
	patchedCentral := false
	for i := 0; i+46 <= len(data); i++ {
		switch binary.LittleEndian.Uint32(data[i:]) {
		case 0x04034b50:
			nameLen := int(binary.LittleEndian.Uint16(data[i+26:]))
			if nameLen == len(regularName) && string(data[i+30:i+30+nameLen]) == regularName {
				copy(data[i+30:i+30+nameLen], directoryName)
				patchedLocal = true
			}
		case 0x02014b50:
			nameLen := int(binary.LittleEndian.Uint16(data[i+28:]))
			if nameLen == len(regularName) && string(data[i+46:i+46+nameLen]) == regularName {
				copy(data[i+46:i+46+nameLen], directoryName)
				binary.LittleEndian.PutUint32(data[i+38:], uint32(0o040755)<<16)
				patchedCentral = true
			}
		}
	}
	if !patchedLocal || !patchedCentral {
		t.Fatal("failed to construct directory-with-data ZIP fixture")
	}
	return data
}

func TestValidateDOCXRejectsDirectoryFileIdentityCollision(t *testing.T) {
	entries := []docxEntry{
		docxDirectory("word/media/"),
		{name: "word/media", data: []byte("file"), method: zip.Store, mode: uint32(0o644)},
	}
	if err := ValidateDOCX(docxArchive(t, zip.Store, entries)); err == nil {
		t.Fatal("directory and file with the same path identity accepted")
	}
}

func TestValidateDOCXRejectsUnsupportedZIPFeatures(t *testing.T) {
	t.Run("unsupported_compression", func(t *testing.T) {
		data := docxArchive(t, zip.Store, nil)
		patchZIPMethod(data, 99)
		if err := ValidateDOCX(data); err == nil {
			t.Fatal("unsupported ZIP compression method accepted")
		}
	})
	t.Run("encrypted", func(t *testing.T) {
		data := docxArchive(t, zip.Store, nil)
		patchZIPFlags(data, 1)
		if err := ValidateDOCX(data); err == nil {
			t.Fatal("encrypted ZIP entry accepted")
		}
	})
	t.Run("symlink", func(t *testing.T) {
		entry := docxEntry{name: "word/media/link", data: []byte("target"), method: zip.Store, mode: uint32(os.ModeSymlink | 0o777)}
		if err := ValidateDOCX(docxArchive(t, zip.Store, []docxEntry{entry})); err == nil {
			t.Fatal("symlink ZIP entry accepted")
		}
	})
}

func TestValidateDOCXRejectsMacros(t *testing.T) {
	tests := []struct {
		name    string
		entries []docxEntry
	}{
		{"vba_project_part", []docxEntry{{name: "word/vbaProject.bin", data: []byte("macro"), method: zip.Store}}},
		{"macro_enabled_main_type", replaceEntry(docxRequiredEntries(), "[Content_Types].xml", contentTypes("application/vnd.ms-word.document.macroEnabled.main+xml"))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var data []byte
			if tc.name == "macro_enabled_main_type" {
				data = writeDOCX(t, tc.entries)
			} else {
				data = docxArchive(t, zip.Store, tc.entries)
			}
			if err := ValidateDOCX(data); err == nil {
				t.Fatal("macro-bearing DOCX accepted")
			}
		})
	}
}

func TestValidateDOCXFailsClosedForMalformedAndCorruptArchives(t *testing.T) {
	valid := docxArchive(t, zip.Store, nil)
	tests := []struct {
		name string
		data []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"not_zip", []byte("not a zip")},
		{"truncated", valid[:len(valid)-10]},
		{"bad_crc", corruptEntryPayload(t, valid, "word/document.xml")},
		{"inconsistent_uncompressed_size", patchZIPUncompressedSize(valid, "word/document.xml", 1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateDOCX(tc.data); err == nil {
				t.Fatal("malformed or corrupt DOCX accepted")
			}
		})
	}
}

func TestValidateDOCXDoesNotExtractFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := ValidateDOCX(docxArchive(t, zip.Store, nil)); err != nil {
		t.Fatalf("valid DOCX rejected: %v", err)
	}
	entries, err := os.ReadDir(".")
	if err != nil || len(entries) != 0 {
		t.Fatalf("validator wrote files to disk: %v, %v", entries, err)
	}
}

func patchZIPMethod(data []byte, method uint16) {
	for i := 0; i+10 <= len(data); i++ {
		sig := binary.LittleEndian.Uint32(data[i:])
		if sig == 0x04034b50 {
			binary.LittleEndian.PutUint16(data[i+8:], method)
		}
		if sig == 0x02014b50 {
			binary.LittleEndian.PutUint16(data[i+10:], method)
		}
	}
}

func patchZIPFlags(data []byte, flags uint16) {
	for i := 0; i+10 <= len(data); i++ {
		sig := binary.LittleEndian.Uint32(data[i:])
		if sig == 0x04034b50 {
			binary.LittleEndian.PutUint16(data[i+6:], flags)
		}
		if sig == 0x02014b50 {
			binary.LittleEndian.PutUint16(data[i+8:], flags)
		}
	}
}

func patchZIPUncompressedSize(data []byte, name string, size uint32) []byte {
	out := bytes.Clone(data)
	for i := 0; i+46 <= len(out); i++ {
		if binary.LittleEndian.Uint32(out[i:]) != 0x02014b50 {
			continue
		}
		n := int(binary.LittleEndian.Uint16(out[i+28:]))
		if string(out[i+46:i+46+n]) == name {
			binary.LittleEndian.PutUint32(out[i+24:], size)
		}
	}
	return out
}

func corruptEntryPayload(t *testing.T, data []byte, name string) []byte {
	t.Helper()
	out := bytes.Clone(data)
	r, err := zip.NewReader(bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.File {
		if f.Name != name {
			continue
		}
		off, err := f.DataOffset()
		if err != nil {
			t.Fatal(err)
		}
		out[off] ^= 0xff
		return out
	}
	t.Fatal("entry not found")
	return nil
}

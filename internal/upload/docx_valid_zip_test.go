package upload

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"testing"
)

func TestValidateDOCXAcceptsEOCDSignatureInsideZIPComment(t *testing.T) {
	data := docxArchive(t, zip.Store, nil)
	comment := make([]byte, 40)
	eocd := len(data) - 22
	copy(comment[5:], data[eocd:eocd+22])
	binary.LittleEndian.PutUint16(comment[5+20:], uint16(len(comment)-(5+22)))
	binary.LittleEndian.PutUint16(data[eocd+20:], uint16(len(comment)))
	data = append(data, comment...)
	docxValidZIPAssertReadable(t, data, len(docxRequiredEntries()))
	if err := ValidateDOCX(data); err != nil {
		t.Fatalf("valid ZIP comment containing an EOCD-shaped sequence rejected: %v", err)
	}
}

func TestValidateDOCXAcceptsBoundedSingleDiskZIP64Members(t *testing.T) {
	data := docxValidZIP64(t, docxRequiredEntries())
	meta := docxValidZIP64Inspect(t, data)
	if meta.records != len(docxRequiredEntries()) || meta.zip64Entries != meta.records || !meta.hasZIP64EOCD || !meta.hasLocator || !meta.hasZIP64Descriptor {
		t.Fatalf("incomplete ZIP64 fixture metadata: %+v", meta)
	}
	docxValidZIP64AssertMemberShapes(t, data, docxRequiredEntries())
	docxValidZIPAssertReadable(t, data, len(docxRequiredEntries()))
	if err := ValidateDOCX(data); err != nil {
		t.Fatalf("valid bounded single-disk ZIP64 DOCX rejected: %v", err)
	}
}

func TestValidateDOCXRejectsZIP64EntryCountOverLimit(t *testing.T) {
	entries := docxRequiredEntries()
	for i := len(entries); i < docxMaxEntries+1; i++ {
		entries = append(entries, docxEntry{name: fmt.Sprintf("word/media/%04d.bin", i), method: zip.Store})
	}
	data := docxValidZIP64(t, entries)
	meta := docxValidZIP64Inspect(t, data)
	if meta.records != docxMaxEntries+1 || meta.zip64Total != uint64(docxMaxEntries+1) {
		t.Fatalf("ZIP64 over-limit fixture metadata = %+v", meta)
	}
	docxValidZIPAssertReadable(t, data, docxMaxEntries+1)
	if err := ValidateDOCX(data); err == nil {
		t.Fatal("valid ZIP64 container with 1001 entries accepted")
	}
}

func TestValidateDOCXRejectsDishonestZIP64ContainerMetadata(t *testing.T) {
	data := docxValidZIP64(t, docxRequiredEntries())
	zip64 := docxValidZIP64EOCDOffset(t, data)
	binary.LittleEndian.PutUint64(data[zip64+32:], uint64(len(docxRequiredEntries())+1))
	meta := docxValidZIP64Inspect(t, data)
	if meta.zip64Total == uint64(meta.records) {
		t.Fatal("fixture ZIP64 count still matches physical records")
	}
	if err := ValidateDOCX(data); err == nil {
		t.Fatal("dishonest ZIP64 total-entry count accepted")
	}
}

func TestValidateDOCXRejectsDishonestZIP64MemberMetadata(t *testing.T) {
	t.Run("offset_enters_central_directory", func(t *testing.T) {
		data := docxValidZIP64(t, docxRequiredEntries())
		centralOffset := docxValidZIP64CentralOffset(t, data)
		extra := docxValidZIP64CentralExtra(t, data, "word/document.xml")
		binary.LittleEndian.PutUint64(extra[20:], uint64(centralOffset))
		if binary.LittleEndian.Uint64(extra[20:]) != uint64(centralOffset) {
			t.Fatal("fixture ZIP64 offset was not patched")
		}
		if err := ValidateDOCX(data); err == nil {
			t.Fatal("ZIP64 member offset entering central directory accepted")
		}
	})
	t.Run("expanded_size_over_limit", func(t *testing.T) {
		data := docxValidZIP64(t, docxRequiredEntries())
		localExtra := docxValidZIP64LocalExtra(t, data, "word/document.xml")
		centralExtra := docxValidZIP64CentralExtra(t, data, "word/document.xml")
		over := uint64(docxMaxEntryBytes + 1)
		binary.LittleEndian.PutUint64(localExtra[4:], over)
		binary.LittleEndian.PutUint64(centralExtra[4:], over)
		if binary.LittleEndian.Uint64(localExtra[4:]) != over || binary.LittleEndian.Uint64(centralExtra[4:]) != over {
			t.Fatal("fixture expanded sizes were not patched")
		}
		if err := ValidateDOCX(data); err == nil {
			t.Fatal("dishonest ZIP64 expanded size above 25 MiB accepted")
		}
	})
}

type docxValidZIP64Metadata struct {
	records, zip64Entries                        int
	zip64Total                                   uint64
	hasZIP64EOCD, hasLocator, hasZIP64Descriptor bool
}

func docxValidZIP64(t *testing.T, entries []docxEntry) []byte {
	t.Helper()
	var local, central bytes.Buffer
	for _, entry := range entries {
		if entry.method != zip.Store {
			t.Fatalf("ZIP64 fixture supports Store only, got %d", entry.method)
		}
		name := []byte(entry.name)
		data := entry.data
		crc := crc32.ChecksumIEEE(data)
		offset := uint64(local.Len())
		localExtra := docxValidZIP64Extra(uint64(len(data)), uint64(len(data)))
		docxValidZIPWriteLocal(t, &local, name, data, crc, localExtra)
		centralExtra := docxValidZIP64Extra(uint64(len(data)), uint64(len(data)), offset)
		docxValidZIPWriteCentral(t, &central, name, crc, centralExtra)
	}
	centralOffset := uint64(local.Len())
	local.Write(central.Bytes())
	zip64Offset := uint64(local.Len())
	docxValidZIPWriteZIP64Trailer(t, &local, uint64(len(entries)), uint64(central.Len()), centralOffset, zip64Offset)
	return local.Bytes()
}

func docxValidZIP64Extra(values ...uint64) []byte {
	extra := make([]byte, 4+8*len(values))
	binary.LittleEndian.PutUint16(extra, 0x0001)
	binary.LittleEndian.PutUint16(extra[2:], uint16(8*len(values)))
	for i, value := range values {
		binary.LittleEndian.PutUint64(extra[4+8*i:], value)
	}
	return extra
}

func docxValidZIPWriteLocal(t *testing.T, out *bytes.Buffer, name, data []byte, crc uint32, extra []byte) {
	t.Helper()
	values := []any{uint32(0x04034b50), uint16(45), uint16(1 << 3), uint16(zip.Store), uint16(0), uint16(0), uint32(0), uint32(0xffffffff), uint32(0xffffffff), uint16(len(name)), uint16(len(extra))}
	for _, value := range values {
		if err := binary.Write(out, binary.LittleEndian, value); err != nil {
			t.Fatal(err)
		}
	}
	out.Write(name)
	out.Write(extra)
	out.Write(data)
	for _, value := range []any{uint32(0x08074b50), crc, uint64(len(data)), uint64(len(data))} {
		if err := binary.Write(out, binary.LittleEndian, value); err != nil {
			t.Fatal(err)
		}
	}
}

func docxValidZIPWriteCentral(t *testing.T, out *bytes.Buffer, name []byte, crc uint32, extra []byte) {
	t.Helper()
	values := []any{uint32(0x02014b50), uint16(45), uint16(45), uint16(1 << 3), uint16(zip.Store), uint16(0), uint16(0), crc, uint32(0xffffffff), uint32(0xffffffff), uint16(len(name)), uint16(len(extra)), uint16(0), uint16(0), uint16(0), uint32(0), uint32(0xffffffff)}
	for _, value := range values {
		if err := binary.Write(out, binary.LittleEndian, value); err != nil {
			t.Fatal(err)
		}
	}
	out.Write(name)
	out.Write(extra)
}

func docxValidZIPWriteZIP64Trailer(t *testing.T, out *bytes.Buffer, count, centralSize, centralOffset, zip64Offset uint64) {
	t.Helper()
	values := []any{uint32(0x06064b50), uint64(44), uint16(45), uint16(45), uint32(0), uint32(0), count, count, centralSize, centralOffset, uint32(0x07064b50), uint32(0), zip64Offset, uint32(1), uint32(0x06054b50), uint16(0), uint16(0), uint16(0xffff), uint16(0xffff), uint32(0xffffffff), uint32(0xffffffff), uint16(0)}
	for _, value := range values {
		if err := binary.Write(out, binary.LittleEndian, value); err != nil {
			t.Fatal(err)
		}
	}
}

func docxValidZIPAssertReadable(t *testing.T, data []byte, wantEntries int) {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("fixture is not valid for locked archive/zip: %v", err)
	}
	if len(r.File) != wantEntries {
		t.Fatalf("archive/zip enumerated %d entries, want %d", len(r.File), wantEntries)
	}
	for _, file := range r.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open %q: %v", file.Name, err)
		}
		if _, err := io.Copy(io.Discard, rc); err != nil {
			t.Fatalf("read %q: %v", file.Name, err)
		}
		if err := rc.Close(); err != nil {
			t.Fatalf("close %q: %v", file.Name, err)
		}
	}
}

func docxValidZIP64Inspect(t *testing.T, data []byte) docxValidZIP64Metadata {
	t.Helper()
	var meta docxValidZIP64Metadata
	for i := 0; i+4 <= len(data); i++ {
		switch binary.LittleEndian.Uint32(data[i:]) {
		case 0x02014b50:
			meta.records++
			n := int(binary.LittleEndian.Uint16(data[i+28:]))
			x := int(binary.LittleEndian.Uint16(data[i+30:]))
			if i+46+n+x <= len(data) && docxValidZIPHasExtra(data[i+46+n:i+46+n+x], 0x0001) {
				meta.zip64Entries++
			}
		case 0x06064b50:
			meta.hasZIP64EOCD = true
			if i+40 <= len(data) {
				meta.zip64Total = binary.LittleEndian.Uint64(data[i+32:])
			}
		case 0x07064b50:
			meta.hasLocator = true
		case 0x08074b50:
			meta.hasZIP64Descriptor = true
		}
	}
	return meta
}

func docxValidZIPHasExtra(extra []byte, id uint16) bool {
	for len(extra) >= 4 {
		n := int(binary.LittleEndian.Uint16(extra[2:]))
		if 4+n > len(extra) {
			return false
		}
		if binary.LittleEndian.Uint16(extra) == id {
			return true
		}
		extra = extra[4+n:]
	}
	return false
}

func docxValidZIP64EOCDOffset(t *testing.T, data []byte) int {
	t.Helper()
	for i := len(data) - 56; i >= 0; i-- {
		if binary.LittleEndian.Uint32(data[i:]) == 0x06064b50 {
			return i
		}
	}
	t.Fatal("ZIP64 EOCD not found")
	return 0
}

func docxValidZIP64CentralOffset(t *testing.T, data []byte) int {
	t.Helper()
	z := docxValidZIP64EOCDOffset(t, data)
	return int(binary.LittleEndian.Uint64(data[z+48:]))
}

func docxValidZIP64LocalExtra(t *testing.T, data []byte, name string) []byte {
	t.Helper()
	for i := 0; i+30 <= len(data); i++ {
		if binary.LittleEndian.Uint32(data[i:]) != 0x04034b50 {
			continue
		}
		n := int(binary.LittleEndian.Uint16(data[i+26:]))
		x := int(binary.LittleEndian.Uint16(data[i+28:]))
		if i+30+n+x <= len(data) && string(data[i+30:i+30+n]) == name {
			return data[i+30+n : i+30+n+x]
		}
	}
	t.Fatalf("local ZIP64 extra for %q not found", name)
	return nil
}

func docxValidZIP64CentralExtra(t *testing.T, data []byte, name string) []byte {
	t.Helper()
	for i := 0; i+46 <= len(data); i++ {
		if binary.LittleEndian.Uint32(data[i:]) != 0x02014b50 {
			continue
		}
		n := int(binary.LittleEndian.Uint16(data[i+28:]))
		x := int(binary.LittleEndian.Uint16(data[i+30:]))
		if i+46+n+x <= len(data) && string(data[i+46:i+46+n]) == name {
			return data[i+46+n : i+46+n+x]
		}
	}
	t.Fatalf("central ZIP64 extra for %q not found", name)
	return nil
}

func docxValidZIP64AssertMemberShapes(t *testing.T, data []byte, entries []docxEntry) {
	t.Helper()
	for _, entry := range entries {
		local, central := docxReviewHeaders(t, data, entry.name)
		if binary.LittleEndian.Uint16(local[6:])&(1<<3) == 0 || binary.LittleEndian.Uint16(central[8:])&(1<<3) == 0 {
			t.Fatalf("%q does not use data-descriptor semantics", entry.name)
		}
		if binary.LittleEndian.Uint32(local[18:]) != 0xffffffff || binary.LittleEndian.Uint32(local[22:]) != 0xffffffff || binary.LittleEndian.Uint32(central[20:]) != 0xffffffff || binary.LittleEndian.Uint32(central[24:]) != 0xffffffff || binary.LittleEndian.Uint32(central[42:]) != 0xffffffff {
			t.Fatalf("%q does not use ZIP64 size and offset sentinels", entry.name)
		}
		localExtra := docxValidZIP64LocalExtra(t, data, entry.name)
		centralExtra := docxValidZIP64CentralExtra(t, data, entry.name)
		if len(localExtra) != 20 || len(centralExtra) != 28 || binary.LittleEndian.Uint64(localExtra[4:]) != uint64(len(entry.data)) || binary.LittleEndian.Uint64(localExtra[12:]) != uint64(len(entry.data)) || binary.LittleEndian.Uint64(centralExtra[4:]) != uint64(len(entry.data)) || binary.LittleEndian.Uint64(centralExtra[12:]) != uint64(len(entry.data)) {
			t.Fatalf("%q has incorrect ZIP64 size extras", entry.name)
		}
		descriptor := 30 + int(binary.LittleEndian.Uint16(local[26:])) + int(binary.LittleEndian.Uint16(local[28:])) + len(entry.data)
		if binary.LittleEndian.Uint32(local[descriptor:]) != 0x08074b50 || binary.LittleEndian.Uint32(local[descriptor+4:]) != crc32.ChecksumIEEE(entry.data) || binary.LittleEndian.Uint64(local[descriptor+8:]) != uint64(len(entry.data)) || binary.LittleEndian.Uint64(local[descriptor+16:]) != uint64(len(entry.data)) {
			t.Fatalf("%q has incorrect ZIP64 data descriptor", entry.name)
		}
	}
}

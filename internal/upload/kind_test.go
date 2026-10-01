package upload

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

// These tests cover provisional type classification, not complete file validity.
// Size, dimensions, pixel decoding, and DOCX archive validation belong to later
// validators. In particular, ZIP magic plus .docx does not prove valid OOXML.
func TestDetectKindAllowlistAndMatchingExtensions(t *testing.T) {
	for _, fixture := range kindFixtures(t) {
		for _, ext := range fixture.extensions {
			for _, filename := range []string{"upload" + ext, "upload" + strings.ToUpper(ext)} {
				t.Run(filename, func(t *testing.T) {
					got, err := DetectKind(filename, fixture.data)
					if err != nil {
						t.Fatalf("matching contents and extension rejected: %v", err)
					}
					if got != fixture.kind {
						t.Errorf("DetectKind = %v, want %v", got, fixture.kind)
					}
				})
			}
		}
	}
	jpegData := kindFixtures(t)[0].data
	if got, err := DetectKind("upload.JpEg", jpegData); err != nil || got != JPEG {
		t.Fatalf("mixed-case JPEG alias: got %v, error %v", got, err)
	}
}

func TestDetectKindRejectsMismatchedAndDisallowedExtensions(t *testing.T) {
	for _, fixture := range kindFixtures(t) {
		for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp", ".pdf", ".docx", ".gif", ".svg", ".zip", ".doc", ".docm", ".xlsx", ".pptx", ".exe", ".txt"} {
			matches := false
			for _, allowed := range fixture.extensions {
				if ext == allowed {
					matches = true
				}
			}
			if matches {
				continue
			}
			t.Run(fixture.name+"_as_"+ext, func(t *testing.T) {
				if _, err := DetectKind("upload"+ext, fixture.data); err == nil {
					t.Fatal("accepted contents with a mismatched or disallowed extension")
				}
			})
		}
	}
}

func TestDetectKindRejectsMissingOrInvalidFinalExtension(t *testing.T) {
	for _, fixture := range kindFixtures(t) {
		ext := fixture.extensions[0]
		for _, filename := range []string{"", "upload", "upload.", "upload" + ext + ".exe", "upload" + ext + " ", "upload" + ext + "\t", "upload" + ext + "\n"} {
			t.Run(fixture.name+"_"+filename, func(t *testing.T) {
				if _, err := DetectKind(filename, fixture.data); err == nil {
					t.Fatal("accepted filename without a matching final extension")
				}
			})
		}
	}
}

func TestDetectKindRejectsDisguisedUnknownAndUnsupportedContents(t *testing.T) {
	contents := []struct {
		name string
		data []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"unknown_binary", []byte{0, 1, 2, 3, 0xff, 0xfe}},
		{"text", []byte("not an uploaded image or document")},
		{"html", []byte("<!DOCTYPE html><script>alert(1)</script>")},
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)},
		{"gif87a", []byte("GIF87a\x01\x00\x01\x00\x00\x00\x00")},
		{"gif89a", []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00")},
		{"bmp", []byte("BM\x00\x00\x00\x00\x00\x00\x00\x00")},
		{"tiff", []byte("II\x2a\x00\x08\x00\x00\x00")},
		{"legacy_office", []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}},
		{"executable", []byte("MZ\x00\x00\x00\x00")},
		{"gzip", []byte{0x1f, 0x8b, 0x08, 0, 0, 0, 0, 0}},
		{"riff_wave", []byte("RIFF\x04\x00\x00\x00WAVE")},
		{"webp_without_riff", []byte("NOPE\x04\x00\x00\x00WEBP")},
	}
	for _, content := range contents {
		for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp", ".pdf", ".docx"} {
			t.Run(content.name+"_as_"+ext, func(t *testing.T) {
				if _, err := DetectKind("upload"+ext, content.data); err == nil {
					t.Fatal("filename extension disguised unknown or unsupported contents")
				}
			})
		}
	}
}

func TestDetectKindSignatureBoundaries(t *testing.T) {
	// Recognition stops at these signatures. Accepting them does not mean the
	// remaining file is complete, decodable, or safe to store or serve.
	cases := []struct {
		name      string
		filename  string
		signature []byte
		kind      Kind
		fixed     []int
	}{
		{"jpeg", "upload.jpg", []byte{0xff, 0xd8, 0xff}, JPEG, []int{0, 1, 2}},
		{"png", "upload.png", []byte("\x89PNG\r\n\x1a\n"), PNG, []int{0, 1, 2, 3, 4, 5, 6, 7}},
		{"webp", "upload.webp", []byte("RIFF\x04\x00\x00\x00WEBP"), WebP, []int{0, 1, 2, 3, 8, 9, 10, 11}},
		{"pdf", "upload.pdf", []byte("%PDF-"), PDF, []int{0, 1, 2, 3, 4}},
		{"docx_candidate", "upload.docx", []byte("PK\x03\x04"), DOCX, []int{0, 1, 2, 3}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := DetectKind(tc.filename, tc.signature); err != nil || got != tc.kind {
				t.Fatalf("complete type signature: got %v, error %v, want %v", got, err, tc.kind)
			}
			for n := 0; n < len(tc.signature); n++ {
				t.Run(fmt.Sprintf("truncated_%d", n), func(t *testing.T) {
					if _, err := DetectKind(tc.filename, tc.signature[:n]); err == nil {
						t.Fatal("accepted incomplete type signature")
					}
				})
			}
			for _, offset := range tc.fixed {
				t.Run(fmt.Sprintf("wrong_byte_%d", offset), func(t *testing.T) {
					data := bytes.Clone(tc.signature)
					data[offset] ^= 0xff
					if _, err := DetectKind(tc.filename, data); err == nil {
						t.Fatal("accepted incorrect type signature")
					}
				})
			}
			for _, prefix := range []string{" ", "\x00", "<html>"} {
				if _, err := DetectKind(tc.filename, append([]byte(prefix), tc.signature...)); err == nil {
					t.Errorf("accepted signature embedded after prefix %q", prefix)
				}
			}
		})
	}
}

func TestDetectKindDOCXClassificationIsProvisional(t *testing.T) {
	// An ordinary ZIP can be a DOCX candidate at this stage. The later DOCX
	// validator must reject it for lacking OOXML entries and content types.
	data := kindTestZIP(t, "notes.txt", "This is not an OOXML document.")
	if got, err := DetectKind("notes.docx", data); err != nil || got != DOCX {
		t.Fatalf("ZIP with .docx should classify provisionally: got %v, error %v", got, err)
	}
	if _, err := DetectKind("notes.zip", data); err == nil {
		t.Fatal("ZIP extension is outside the allowlist")
	}
}

type kindTestFixture struct {
	name       string
	kind       Kind
	extensions []string
	data       []byte
}

func kindFixtures(t *testing.T) []kindTestFixture {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	var jpg, pngData bytes.Buffer
	if err := jpeg.Encode(&jpg, img, nil); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&pngData, img); err != nil {
		t.Fatal(err)
	}
	return []kindTestFixture{
		{"jpeg", JPEG, []string{".jpg", ".jpeg"}, jpg.Bytes()},
		{"png", PNG, []string{".png"}, pngData.Bytes()},
		// The WebP and PDF fixtures need only enough bytes for type recognition.
		// RIFF's size bytes vary and are not part of the WebP magic.
		{"webp", WebP, []string{".webp"}, []byte("RIFF\x20\x01\x00\x00WEBPVP8X")},
		{"pdf", PDF, []string{".pdf"}, []byte("%PDF-1.7\n")},
		{"docx", DOCX, []string{".docx"}, kindTestZIP(t, "word/document.xml", `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body/></w:document>`)},
	}
}

func kindTestZIP(t *testing.T, name, contents string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	entry, err := writer.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(contents)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

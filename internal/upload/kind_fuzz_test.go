package upload

import (
	"bytes"
	"fmt"
	"testing"
)

const (
	maxFuzzFilenameBytes = 4 << 10
	maxFuzzDataBytes     = 1 << 20
)

type detectKindSeed struct {
	name     string
	filename string
	data     []byte
	want     Kind
	wantErr  bool
}

func TestDetectKindFuzzSeeds(t *testing.T) {
	for _, seed := range detectKindSeeds() {
		t.Run(seed.name, func(t *testing.T) {
			got, err := DetectKind(seed.filename, seed.data)
			if seed.wantErr {
				if err == nil {
					t.Fatalf("DetectKind(%q, %x) = %q, nil; want rejection", seed.filename, seed.data, got)
				}
				if got != "" {
					t.Fatalf("DetectKind(%q, %x) returned kind %q with error %v; want empty kind", seed.filename, seed.data, got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("DetectKind(%q, %x) returned error: %v", seed.filename, seed.data, err)
			}
			if got != seed.want {
				t.Fatalf("DetectKind(%q, %x) = %q; want %q", seed.filename, seed.data, got, seed.want)
			}
		})
	}
}

func FuzzDetectKind(f *testing.F) {
	for _, seed := range detectKindSeeds() {
		f.Add(seed.filename, seed.data)
	}

	allowed := map[Kind]bool{
		JPEG: true,
		PNG:  true,
		WebP: true,
		PDF:  true,
		DOCX: true,
	}

	f.Fuzz(func(t *testing.T, filename string, data []byte) {
		// Inputs beyond these explicit bounds are outside this target's corpus.
		// Every input within the bounds reaches DetectKind without truncation.
		if len(filename) > maxFuzzFilenameBytes || len(data) > maxFuzzDataBytes {
			return
		}

		original := bytes.Clone(data)
		kind, err := DetectKind(filename, data)
		if !bytes.Equal(data, original) {
			t.Fatal("DetectKind mutated its input bytes")
		}

		againKind, againErr := DetectKind(filename, data)
		if againKind != kind || fmt.Sprint(againErr) != fmt.Sprint(err) {
			t.Fatalf("DetectKind is nondeterministic: first=(%q, %v), second=(%q, %v)", kind, err, againKind, againErr)
		}

		if err != nil {
			if kind != "" {
				t.Fatalf("rejection returned kind %q with error %v", kind, err)
			}
			return
		}
		if !allowed[kind] {
			t.Fatalf("success returned kind %q outside the upload allowlist", kind)
		}

		mismatchedKind, mismatchedErr := DetectKind(filename+".mismatch", data)
		if mismatchedErr == nil || mismatchedKind != "" {
			t.Fatalf("adding a mismatched final extension manufactured success: kind=%q err=%v", mismatchedKind, mismatchedErr)
		}

		withSuffix := append(bytes.Clone(data), 0xa5)
		suffixKind, suffixErr := DetectKind(filename, withSuffix)
		if suffixErr != nil || suffixKind != kind {
			t.Fatalf("trailing content changed leading-signature result: before=%q after=(%q, %v)", kind, suffixKind, suffixErr)
		}

		prefixed := append([]byte{0}, data...)
		prefixKind, prefixErr := DetectKind(filename, prefixed)
		if prefixErr == nil || prefixKind != "" {
			t.Fatalf("moving the signature away from byte zero was accepted: kind=%q err=%v", prefixKind, prefixErr)
		}

		truncatedKind, truncatedErr := DetectKind(filename, nil)
		if truncatedErr == nil || truncatedKind != "" {
			t.Fatalf("empty truncation was accepted: kind=%q err=%v", truncatedKind, truncatedErr)
		}
	})
}

func detectKindSeeds() []detectKindSeed {
	return []detectKindSeed{
		{name: "jpeg_jpg", filename: "photo.jpg", data: []byte{0xff, 0xd8, 0xff}, want: JPEG},
		{name: "jpeg_alias", filename: "photo.jpeg", data: []byte{0xff, 0xd8, 0xff, 0x00}, want: JPEG},
		{name: "jpeg_mixed_case_double_extension", filename: "photo.png.JpG", data: []byte{0xff, 0xd8, 0xff}, want: JPEG},
		{name: "png", filename: "image.png", data: []byte("\x89PNG\r\n\x1a\n"), want: PNG},
		{name: "png_binary_unicode_filename", filename: "\x00snowman-☃.PNG", data: []byte("\x89PNG\r\n\x1a\nrest"), want: PNG},
		{name: "webp", filename: "image.webp", data: []byte("RIFF\x00\x00\x00\x00WEBP"), want: WebP},
		{name: "pdf", filename: "paper.pdf", data: []byte("%PDF-"), want: PDF},
		{name: "docx_provisional_zip_marker", filename: "draft.docx", data: []byte{'P', 'K', 0x03, 0x04}, want: DOCX},
		{name: "nil", filename: "empty.pdf", data: nil, wantErr: true},
		{name: "empty", filename: "empty.png", data: []byte{}, wantErr: true},
		{name: "truncated_jpeg", filename: "short.jpg", data: []byte{0xff, 0xd8}, wantErr: true},
		{name: "truncated_png", filename: "short.png", data: []byte("\x89PNG\r\n\x1a"), wantErr: true},
		{name: "truncated_webp", filename: "short.webp", data: []byte("RIFF\x00\x00\x00\x00WEB"), wantErr: true},
		{name: "truncated_pdf", filename: "short.pdf", data: []byte("%PDF"), wantErr: true},
		{name: "truncated_docx", filename: "short.docx", data: []byte{'P', 'K', 0x03}, wantErr: true},
		{name: "wrong_signature", filename: "wrong.png", data: []byte("not a png"), wantErr: true},
		{name: "extension_mismatch", filename: "image.jpg", data: []byte("\x89PNG\r\n\x1a\n"), wantErr: true},
		{name: "disallowed_extension", filename: "image.gif", data: []byte("\x89PNG\r\n\x1a\n"), wantErr: true},
		{name: "missing_extension", filename: "image", data: []byte("\x89PNG\r\n\x1a\n"), wantErr: true},
		{name: "double_extension_final_mismatch", filename: "image.png.exe", data: []byte("\x89PNG\r\n\x1a\n"), wantErr: true},
		{name: "hostile_bytes", filename: "payload.pdf", data: []byte{0x00, 0xff, 0x00, '\n', '\r', 0x7f}, wantErr: true},
	}
}

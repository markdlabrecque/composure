package upload

import "testing"

func TestStorageNameUsesLowercaseSHA256OfStoredBytes(t *testing.T) {
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad.png"

	got, err := StorageName([]byte("abc"), PNG)
	if err != nil {
		t.Fatalf("StorageName returned an error: %v", err)
	}
	if got != want {
		t.Fatalf("StorageName = %q, want %q", got, want)
	}
}

func TestStorageNameCanonicalExtensions(t *testing.T) {
	const digest = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	tests := []struct {
		name string
		kind Kind
		ext  string
	}{
		{name: "jpeg", kind: JPEG, ext: ".jpg"},
		{name: "png", kind: PNG, ext: ".png"},
		{name: "pdf", kind: PDF, ext: ".pdf"},
		{name: "docx", kind: DOCX, ext: ".docx"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := StorageName([]byte("abc"), tc.kind)
			if err != nil {
				t.Fatalf("StorageName returned an error: %v", err)
			}
			if want := digest + tc.ext; got != want {
				t.Errorf("StorageName = %q, want %q", got, want)
			}
		})
	}
}

func TestStorageNameIsDeterministicAndContentAddressed(t *testing.T) {
	stored := []byte("same validated stored bytes")
	first, err := StorageName(stored, PDF)
	if err != nil {
		t.Fatalf("first StorageName call returned an error: %v", err)
	}
	second, err := StorageName(stored, PDF)
	if err != nil {
		t.Fatalf("second StorageName call returned an error: %v", err)
	}
	if first != second {
		t.Fatalf("same stored bytes produced different names: %q and %q", first, second)
	}

	changed, err := StorageName([]byte("different validated stored bytes"), PDF)
	if err != nil {
		t.Fatalf("StorageName for changed content returned an error: %v", err)
	}
	if changed == first {
		t.Fatalf("different stored bytes produced the same name %q", first)
	}
}

func TestStorageNameHashesPostReencodingImageBytes(t *testing.T) {
	rawUpload := []byte("raw webp upload")
	storedPNG := []byte("stored pixels after re-encoding")
	const want = "17e52ed590cb63ebee871fbc39e941e866887bd6aede89520a853d2f68a98397.png"

	got, err := StorageName(storedPNG, PNG)
	if err != nil {
		t.Fatalf("StorageName returned an error: %v", err)
	}
	if got != want {
		t.Fatalf("StorageName = %q, want hash of re-encoded PNG bytes %q", got, want)
	}
	rawUploadName, err := StorageName(rawUpload, PNG)
	if err != nil {
		t.Fatalf("StorageName for raw upload comparison returned an error: %v", err)
	}
	if got == rawUploadName {
		t.Fatalf("StorageName hashed raw upload bytes instead of stored PNG bytes: %q", got)
	}
}

func TestStorageNameRejectsUnsupportedStoredKind(t *testing.T) {
	for _, kind := range []Kind{WebP, Kind("gif")} {
		t.Run(string(kind), func(t *testing.T) {
			got, err := StorageName([]byte("validated stored bytes"), kind)
			if err == nil {
				t.Fatal("StorageName accepted an unsupported stored kind")
			}
			if got != "" {
				t.Errorf("StorageName returned %q with an error, want no name", got)
			}
		})
	}
}

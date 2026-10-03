package upload

import (
	"archive/zip"
	"testing"
)

func TestValidateDOCXRequiresContentTypeDeclarationAttributes(t *testing.T) {
	tests := []struct {
		name        string
		declaration string
	}{
		{"default_missing_extension", `<Default ContentType="application/octet-stream"/>`},
		{"default_empty_extension", `<Default Extension="" ContentType="application/octet-stream"/>`},
		{"default_missing_content_type", `<Default Extension="bin"/>`},
		{"default_empty_content_type", `<Default Extension="bin" ContentType=""/>`},
		{"override_missing_part_name", `<Override ContentType="application/octet-stream"/>`},
		{"override_empty_part_name", `<Override PartName="" ContentType="application/octet-stream"/>`},
		{"override_missing_content_type", `<Override PartName="/word/extra.bin"/>`},
		{"override_empty_content_type", `<Override PartName="/word/extra.bin" ContentType=""/>`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entries := replaceEntry(docxRequiredEntries(), "[Content_Types].xml", docxSettlementContentTypes(tc.declaration))
			entries = append(entries, docxEntry{name: "word/extra.bin", data: []byte("ordinary"), method: zip.Store})
			if err := ValidateDOCX(writeDOCX(t, entries)); err == nil {
				t.Fatal("content type declaration with a missing or empty required attribute accepted")
			}
		})
	}
}

func TestValidateDOCXRejectsDuplicateDefaultExtensionsCaseInsensitively(t *testing.T) {
	contentTypes := docxSettlementContentTypes(`<Default Extension="bin" ContentType="application/octet-stream"/><Default Extension="BIN" ContentType="application/example"/>`)
	entries := replaceEntry(docxRequiredEntries(), "[Content_Types].xml", contentTypes)
	entries = append(entries, docxEntry{name: "word/extra.bin", data: []byte("ordinary"), method: zip.Store})
	if err := ValidateDOCX(writeDOCX(t, entries)); err == nil {
		t.Fatal("case-insensitive duplicate Default extensions accepted")
	}
}

func TestValidateDOCXRequiresRelationshipAttributes(t *testing.T) {
	tests := []struct {
		name         string
		relationship string
	}{
		{"missing_id", `<Relationship Type="urn:example:metadata" Target="docProps/core.xml"/>`},
		{"empty_id", `<Relationship Id="" Type="urn:example:metadata" Target="docProps/core.xml"/>`},
		{"missing_type", `<Relationship Id="rExtra" Target="docProps/core.xml"/>`},
		{"empty_type", `<Relationship Id="rExtra" Type="" Target="docProps/core.xml"/>`},
		{"missing_target", `<Relationship Id="rExtra" Type="urn:example:metadata"/>`},
		{"empty_target", `<Relationship Id="rExtra" Type="urn:example:metadata" Target=""/>`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rels := docxSettlementRelationships(tc.relationship)
			entries := replaceEntry(docxRequiredEntries(), "_rels/.rels", rels)
			entries = append(entries, docxEntry{name: "docProps/core.xml", data: []byte("ordinary"), method: zip.Store})
			if err := ValidateDOCX(writeDOCX(t, entries)); err == nil {
				t.Fatal("relationship with a missing or empty required attribute accepted")
			}
		})
	}
}

func TestValidateDOCXRejectsDuplicateRelationshipIDs(t *testing.T) {
	rels := docxSettlementRelationships(`<Relationship Id="rId1" Type="urn:example:metadata" Target="docProps/core.xml"/>`)
	entries := replaceEntry(docxRequiredEntries(), "_rels/.rels", rels)
	entries = append(entries, docxEntry{name: "docProps/core.xml", data: []byte("ordinary"), method: zip.Store})
	if err := ValidateDOCX(writeDOCX(t, entries)); err == nil {
		t.Fatal("duplicate relationship Id accepted")
	}
}

func TestValidateDOCXAcceptsDistinctContentTypesAndRelationshipIDs(t *testing.T) {
	contentTypes := docxSettlementContentTypes(`<Default Extension="bin" ContentType="application/octet-stream"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>`)
	rels := docxSettlementRelationships(`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/><Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="word/styles.xml"/>`)
	entries := replaceEntry(docxRequiredEntries(), "[Content_Types].xml", contentTypes)
	entries = replaceEntry(entries, "_rels/.rels", rels)
	entries = append(entries,
		docxEntry{name: "docProps/core.xml", data: []byte("ordinary core properties"), method: zip.Store},
		docxEntry{name: "word/styles.xml", data: []byte("ordinary styles"), method: zip.Store},
		docxEntry{name: "word/media/image.bin", data: []byte("ordinary binary data"), method: zip.Store},
	)
	if err := ValidateDOCX(writeDOCX(t, entries)); err != nil {
		t.Fatalf("ordinary extra parts and distinct relationship IDs rejected: %v", err)
	}
}

func docxSettlementContentTypes(extra string) []byte {
	return []byte(`<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` + extra + `</Types>`)
}

func docxSettlementRelationships(extra string) []byte {
	return []byte(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` + extra + `</Relationships>`)
}

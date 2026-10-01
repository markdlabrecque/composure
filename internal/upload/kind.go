package upload

import (
	"errors"
	"strings"
)

// Kind is a provisionally detected upload format. Detection checks only the
// format's leading signature; it does not validate complete file contents.
type Kind string

const (
	JPEG Kind = "jpeg"
	PNG  Kind = "png"
	WebP Kind = "webp"
	PDF  Kind = "pdf"
	DOCX Kind = "docx"
)

var (
	errUnsupportedContent  = errors.New("unsupported or incomplete file signature")
	errMismatchedExtension = errors.New("filename extension does not match detected file type")
)

// DetectKind classifies an allowlisted file using its leading signature and
// requires a matching filename extension. DOCX classification is provisional:
// ZIP magic and a .docx extension do not establish that the archive is valid
// OOXML. This function does not approve files for storage or serving.
func DetectKind(filename string, data []byte) (Kind, error) {
	var kind Kind
	switch {
	case len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
		kind = JPEG
	case len(data) >= 8 &&
		data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G' &&
		data[4] == '\r' && data[5] == '\n' && data[6] == 0x1a && data[7] == '\n':
		kind = PNG
	case len(data) >= 12 &&
		data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F' &&
		data[8] == 'W' && data[9] == 'E' && data[10] == 'B' && data[11] == 'P':
		kind = WebP
	case len(data) >= 5 && data[0] == '%' && data[1] == 'P' && data[2] == 'D' && data[3] == 'F' && data[4] == '-':
		kind = PDF
	case len(data) >= 4 && data[0] == 'P' && data[1] == 'K' && data[2] == 0x03 && data[3] == 0x04:
		kind = DOCX
	default:
		return "", errUnsupportedContent
	}

	if !extensionMatches(filename, kind) {
		return "", errMismatchedExtension
	}
	return kind, nil
}

func extensionMatches(filename string, kind Kind) bool {
	dot := strings.LastIndexByte(filename, '.')
	if dot < 0 {
		return false
	}
	ext := filename[dot:]
	switch kind {
	case JPEG:
		return strings.EqualFold(ext, ".jpg") || strings.EqualFold(ext, ".jpeg")
	case PNG:
		return strings.EqualFold(ext, ".png")
	case WebP:
		return strings.EqualFold(ext, ".webp")
	case PDF:
		return strings.EqualFold(ext, ".pdf")
	case DOCX:
		return strings.EqualFold(ext, ".docx")
	default:
		return false
	}
}

# Upload kind detector fuzz check

`internal/upload.DetectKind` classifies a filename and its leading bytes as one
of JPEG, PNG, WebP, PDF, or DOCX. This fuzz target checks that classification
does not panic, is deterministic, leaves input bytes unchanged, and returns
only an allowed kind on success (or an empty kind on error). It also checks
extension mismatch, trailing-byte, leading-signature, and empty-truncation
properties. Twenty-one fixed synthetic seeds exercise accepted signatures,
aliases, case and double extensions, mismatches, missing and disallowed
extensions, empty and truncated inputs, and hostile bytes.

DOCX success here means only that the current detector recognizes its
provisional ZIP marker with a `.docx` filename. It does not establish a valid
ZIP archive, OOXML package, or safe document. Similarly, image seeds test
signature classification only; this target does not decode images or validate
dimensions, file size, or content structure. Those behaviors belong to their
respective validators and later checks.

Use Go 1.27.1 and run from the repository root. The seed test is deterministic
and runs with ordinary package tests:

```sh
go test ./internal/upload -run=^TestDetectKindFuzzSeeds$ -count=1
```

Run one bounded fuzz campaign with two workers and a 60-second overall test
timeout:

```sh
go test ./internal/upload -run=^$ -fuzz=^FuzzDetectKind$ -fuzztime=10s -parallel=2 -timeout=60s
```

The target submits generated filenames up to 4 KiB and data up to 1 MiB to
`DetectKind` without truncation. Inputs larger than either bound are outside
the target and are skipped. The ten-second run is a bounded local check, not
exhaustive coverage or a persistent discovery campaign. It does not run in CI.
The campaign does not fuzz upload size or image-dimension validators and does
not fuzz DOCX ZIP/OOXML validation. Do not infer those guarantees from a green
result.

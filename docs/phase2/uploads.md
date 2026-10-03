# Upload contract

This document records the upload rules that later implementation tickets must follow. It defines future behavior; Phase 1 does not implement uploads. The [Phase 1 Page configuration format](../phase1/page-config-v1.md) is closed and rejects unknown properties, so upload settings need an explicitly versioned configuration change before sites can override limits.

## Accepted files and size limits

Accept JPEG, PNG, WebP, PDF, and DOCX. Validate the file contents and require the filename extension to match the detected format. Reject other formats and mismatches with an error that lets the editor correct the upload.

The default limits are 25 MiB globally, 10 MiB for images, and 25 MiB for documents. Convert MiB to bytes using 1 MiB = 1,048,576 bytes:

| Setting | Default | Bytes |
| --- | ---: | ---: |
| Global | 25 MiB | 26,214,400 |
| Image | 10 MiB | 10,485,760 |
| Document | 25 MiB | 26,214,400 |

The effective limit is the smaller of the global limit and the matching image or document limit. A file at the limit is accepted; a file one byte larger is rejected. For example, with defaults an image of 10,485,760 bytes is accepted and one of 10,485,761 bytes is rejected. If the global limit is 8 MiB and the image limit is 12 MiB, the effective image limit is 8 MiB. If the global limit is 30 MiB and the document limit is 20 MiB, the effective document limit is 20 MiB.

An optional field-specific limit may lower the effective global/category limit. It cannot raise that ceiling: an absent field limit has no effect, and an equal or larger field limit leaves the global/category limit in force. The size validator takes explicit global, image, and document limits plus this optional field limit; it does not load or configure them. Site configuration may override the global/category limits only after an explicitly versioned configuration change. The property names and migration are not defined here; do not add guessed keys to the closed Phase 1 format.

The validator rejects negative file sizes, unsupported kinds, and non-positive supplied limits as invalid input. Limits and file sizes are byte counts represented as signed 64-bit integers; comparisons do not calculate derived sizes and remain safe at the maximum representable value.

## Images

Before decoding pixels, inspect JPEG, PNG, and WebP headers and reject malformed or truncated headers, zero dimensions, and an image if either dimension exceeds 16,384 pixels or if width multiplied by height exceeds 40,000,000 pixels. These are immutable hard ceilings: supplied site limits and optional field-specific limits may independently lower the width, height, or pixel ceiling, but cannot raise it. Header inspection must be bounded to at most 1 MiB of input and must not decode pixels. Check the arithmetic without overflow. A 5,000 × 8,000 image is exactly 40,000,000 pixels and meets the area limit; 5,001 × 8,000 exceeds it. A width of 16,385 pixels exceeds the axis limit even when the other dimension is 1 pixel.

Decode and re-encode accepted images without EXIF or GPS metadata. Decode WebP with pinned `golang.org/x/image/webp` and store it as PNG. Use Go's standard library for JPEG and PNG encoding. Generate image variants with `golang.org/x/image/draw` as bounded editorial work before publication, never in response to a public request. Initially, one image worker handles jobs per site.

## DOCX archives

Treat DOCX as an untrusted ZIP archive. Inspect and validate it without extracting files to disk. Accept at most 1,000 entries, including directory entries; at most 100 MiB of total expanded data; and at most 25 MiB for any one entry. Accept an expanded-to-compressed ratio of at most 100:1 for each entry and for the archive overall. These limits are inclusive.

Accept only stored and deflated entries. Reject encrypted entries, macro-enabled content, symbolic links, duplicate paths, unsafe paths, malformed or truncated archives, inconsistent size metadata, and entries whose data or CRC cannot be fully read and verified. Safe directory entries with a terminal slash are allowed only when they have consistent directory metadata and no compressed or expanded member data; they count toward the entry limit. Reject unsafe directories, file/directory entries that identify the same path, and files that conflict with child paths. Require valid OOXML content types, one internal officeDocument relationship to the main part, and a well-formed Word main document. Bound actual reads by the entry and archive limits and check size arithmetic for overflow. Never execute macros or extract archive entries to disk.

The DOCX validator accepts single-disk ZIP archives, including bounded ZIP64
containers and members, and rejects multi-disk archives and archives with
prepended data. It checks EOCD and central-directory counts and bounds before
allocating member metadata, then verifies each local header and optional data
descriptor against its central record. It rejects overlapping member ranges
and checks CRC-32 for every expanded entry. Macro checks include content-type
Default and Override declarations and package relationship types, including
renamed VBA targets.
This is structural OOXML validation: it does not validate the full
WordprocessingML schema or document semantics.

## Names, references, and recovery

Never use an uploaded filename to form a storage name. Store files under the site's `files/` directory using a content hash of the validated bytes that are actually stored. Names are immutable; replacing a file creates another file and leaves the earlier bytes untouched. For images, hash the re-encoded stored bytes, including WebP input after conversion to PNG. The hash algorithm and filename layout are not specified by this contract.

Persist the complete file before committing a database reference. A failed reference transaction may leave an orphan file, but must never commit a reference to a missing file. Drafts and immutable published snapshots hold file references; Trash keeps its references too. Garbage collection runs separately after the transaction and may delete a file only when it has no draft, snapshot, Trash, or rollback-snapshot references and it has remained unreferenced beyond the configured recovery window, 30 days by default. A recreated reference resets that age. Disable collection if the retained recovery set cannot be established. These rules follow the [runtime content and deployment ADR](../adr/0001-runtime-content-and-deployment.md) and the [Phase 1 file-reference contract](../phase1/content-contract.md#10-future-capabilities).

## Serving and source precedence

Serve PDF and DOCX through a dedicated media route as downloads, with `Content-Disposition: attachment` and `X-Content-Type-Options: nosniff`. Never serve documents as inline HTML. Serve processed image bytes for image placements.

The PRD and runtime content ADR define content-hash names. The draft for P2-54 (#141) says to generate random names; that conflicts with the accepted PRD and ADR and must be corrected before implementation. This contract follows the PRD and ADR. It does not choose a hash algorithm or storage path layout beyond the established `<site>/files/` directory.

## Source and implementation status

- [PRD, FR-07](../prd.md#functional-requirements) and [upload security requirements](../prd.md#security-and-privacy) define formats, image handling, default limits, serving headers, and the DOCX checks.
- [Runtime content and deployment ADR](../adr/0001-runtime-content-and-deployment.md#files-and-recovery) defines content-hash names, immutability, reference ordering, locking, and recovery-safe collection.
- [Phase 1 content contract, future file references](../phase1/content-contract.md#10-future-capabilities) defines file references and retention alongside drafts, snapshots, and Trash.
- [Phase 1 Page configuration format](../phase1/page-config-v1.md#change-and-deployment-boundary) is closed and does not currently define upload settings.

These are contracts for later work, not claims that upload handling exists today. Implementation must resolve the DOCX bounds and the P2-54 ticket mismatch before the affected validators or storage naming are built.

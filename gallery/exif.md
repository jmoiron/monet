# EXIF and image decoding for the gallery: options

The gallery needs two things from an image file: the **metadata** in it (EXIF and
friends) and a way to **decode it** to make the web-sized copy and thumbnail. They
are separate problems with separate options. This is what we know, what's open, and
what each choice would cost.

## What the gallery wants from a file

Resolution (as displayed), time taken (ideally with a UTC offset), file size,
camera make and model, lens, exposure (focal length, aperture, shutter, ISO), GPS
(and altitude), software, orientation, and a caption if the file has one
(`ImageDescription`, XMP/IPTC title and description).

## What's in the library today

Checked on this machine (`/mnt/omocha`):

| Kind | Count | Notes |
|---|---|---|
| Canon CR2 (RAW) | 6,962 | older bodies (Rebel XTi, T2i, ...); embedded `PreviewImage` is small (~140 KB on the one checked) |
| Canon CR3 (RAW) | 11 | R5 Mark II; has `PreviewImage` (~390 KB) and `JpgFromRaw` (~5 MB, near full size) |
| Canon HIF (HEIF) | 3 | R5 Mark II, 8192x5464, ~34 MB; exiftool reads it, Go can't decode it |
| iPhone HEIC | 0 | none; the only two `.heic` files are test fixtures in a Go module cache |
| Instagram export | 294 photos | JPEG; **no EXIF at all** in any file (Instagram strips it) |

So for Instagram, camera/lens/exposure will always be empty whatever we pick; what
exists is GPS, a date, and a device id from the export's JSON. EXIF matters for
**uploads** (JPEG from cameras, and RAW/HIF if we want those), and for Google Photos
only through its API (its downloads drop GPS).

## Metadata: the options

### A. Pure Go: `rwcarlsen/goexif` (what the gallery uses now)

- **For:** no external program, trivial to deploy, fast (in process), tested here.
- **Reads:** JPEG and TIFF only. Not PNG, WebP, HEIC/HEIF, CR3, or video. CR2 is
  TIFF-based, so likely partly readable, but that is untested.
- **Gaps:** lens only if it is in the standard EXIF tag (a Canon EOS Rebel T2i with
  an EF 50mm: goexif found no lens, exiftool found `EF50mm f/1.8 II`, because Canon
  keeps it in the MakerNote); no XMP/IPTC; unmaintained since 2019.
- Works today and is covered by tests (`gallery/exif.go`, `gallery/exif_test.go`).

### B. `exiftool` (the Perl program) through `barasher/go-exiftool` (what metasync does)

- **How:** one long-lived `exiftool -stay_open` process per worker, files sent in
  batches, JSON back with numeric values (`-n`). metasync uses 4 workers, batches
  of 4, and a 16 MB buffer (`../metasync/exif/exif.go`).
- **For:** reads essentially everything (JPEG, PNG, WebP, HEIC/HEIF, CR2, CR3, other
  RAW, video), including MakerNotes (lens), XMP, IPTC, timezone offsets, and it can
  *write* tags (the gallery doesn't need that). Extracts embedded RAW previews.
- **Speed:** startup is ~58 ms per process, so per-file launches are slow (20 files
  one at a time: 1.16 s) while one batch is fast (62 files in 0.26 s). The stay-open
  mode is what makes it fast.
- **Against:** an external dependency. `apt install libimage-exiftool-perl` (here:
  13.50, Perl 5.40). Whether the server has it is unknown. A subprocess to manage.
- **Lifecycle in a long-running server** (metasync is a short-lived tool, so it
  doesn't solve this): start lazily, close on shutdown, restart if it dies, and a
  timeout so one bad file can't hang uploads. For a single upload, one process per
  file (~60 ms) is acceptable and avoids all of that; for an import of hundreds of
  files, use a batch.

### C. Other pure-Go libraries

Not evaluated in depth. `evanoberholster/imagemeta` (supports JPEG, HEIC, AVIF, WebP,
PNG, RAW variants per its README) installs fine, but its API differed from what I
guessed and I didn't get it running, so I can't say how good it is. `dsoprea/go-exif`
was not looked at. Either could replace A if we want pure Go with broader formats;
worth an hour's evaluation against real files before relying on it.

### D. Hybrid (recommended)

One small `Extractor` interface returning a normalized struct; use exiftool when it
is on `PATH`, otherwise goexif. Nothing breaks if exiftool is missing; with it, you
get lens, MakerNotes, offsets, captions, and RAW/HIF/PNG/WebP metadata.

- Put it in a small `exif` package in monet. metasync's own `exif` package imports
  metasync's `model`, `progress` and `spool`, so it can't be imported as is, and
  metasync already depends on monet (`replace ../monet`), so monet cannot import
  metasync. A package in monet that metasync imports is the direction that works.
- Normalized fields: take metasync's `normalize()` (date + offset fallbacks, GPS
  from several tags and refs, aperture/exposure/focal/ISO) and add: lens
  (`LensModel`, falling back to `LensID`), make, software, orientation, altitude,
  and caption (`ImageDescription`, `XMP:Description`, `IPTC:Caption-Abstract`).

## Decoding and thumbnails: separate from metadata

Go's standard library (+ `x/image`) decodes JPEG, PNG, GIF and WebP (not animated
WebP). exiftool doesn't change that; it can only hand over embedded previews.

| Format | Option |
|---|---|
| JPEG, PNG, GIF, WebP | decode in Go (done) |
| RAW (CR2/CR3/NEF/ARW/...) | extract the embedded JPEG with exiftool (`PreviewImage`, `JpgFromRaw`, `ThumbnailImage`, as metasync's `scan/img/thumbs.go` does) and use that as the "original" for the web copy. CR3's `JpgFromRaw` is near full size; the CR2 preview checked is small (~140 KB), so CR2 web copies would be small unless the file has a larger `JpgFromRaw`. Needs exiftool, so not available under option A. |
| HEIF/HEIC | not decodable in Go. Needs `libheif` (`heif-convert`), `vips`, or ImageMagick (`apt install libheif-examples`, etc.). Only 3 files today; none are iPhone HEIC. |
| Video (Instagram reels, mp4) | needs `ffmpeg` for thumbnails. The Instagram import currently skips video. |

## Privacy: unrelated to the library choice, but needs fixing

The web-sized copy and thumbnail are re-encoded and carry no metadata. The
**original file is still served and linked from the public detail page**, and for an
upload that includes its EXIF, including GPS. Options: don't link originals
publicly; or serve them only when the photo is marked to; or strip metadata from
the stored/served original (needs exiftool, or a JPEG segment stripper). Instagram
originals are already stripped.

## Time taken

EXIF dates have no zone. The gallery stores the camera's wall clock as if it were
UTC, which is wrong by the zone's offset for display math but stable and matches
what the camera showed. exiftool-based extraction provides `OffsetTimeOriginal` for
newer cameras (checked: the R5 II's CR3 and HIF files carry `OffsetTimeOriginal`),
which would let us store the true instant. Caveat, also checked: the Hong Kong and
Tokyo files say `-05:00`, the camera's own zone setting from home, not where the
photo was taken; that is the kind of error metasync exists to fix. The offset is
only as right as the camera's clock setting. The CR2 checked has no offset, so the
wall-clock rule stays for older bodies.

## Open decisions

1. Hybrid (D), or just keep goexif (A) for now?
2. RAW in the gallery: wanted at all? If yes it requires exiftool, and a decision
   on whether the "original" for RAW is the embedded preview.
3. HIF/HEIC: three files today; worth adding a `libheif` dependency, or skip?
4. Public originals: remove the link, gate it, or strip metadata?
5. Whether to evaluate option C before building D.

## What was checked, and what wasn't

Checked here: the file counts and camera metadata above (`exiftool 13.50`); goexif
against a synthetic JPEG with EXIF and a real Canon JPEG; the exiftool timing; the
metasync code read-through (not run). Not checked: exiftool availability on the
server, goexif on CR2/CR3, option C's libraries, CR2 `JpgFromRaw` sizes, and
`heif-convert` quality or speed.

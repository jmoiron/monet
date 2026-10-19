package gallery

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	// the long edge of the web sized copy, made only for originals bigger than it
	largeEdge = 1920
	// the long edge of the thumbnail
	thumbEdge = 800

	jpegQuality = 85
)

// A Store keeps image files on disk, in a directory that the website also
// serves (the "photos" filesystem), under paths relative to it.
type Store struct {
	root string
}

// NewStore returns a store rooted at the directory root, creating it if need be.
func NewStore(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &Store{root: root}, nil
}

// Root is the directory the store keeps its files in.
func (s *Store) Root() string { return s.root }

// path is where rel is on disk, or an error if it would be outside the store.
func (s *Store) path(rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if rel == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%q is not a path in the photo store", rel)
	}
	return filepath.Join(s.root, clean), nil
}

// Write stores data at rel, replacing what was there. It is written whole or
// not at all.
func (s *Store) Write(rel string, data []byte) error {
	p, err := s.path(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := firstErr(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// Exists reports whether there is a file at rel.
func (s *Store) Exists(rel string) bool {
	p, err := s.path(rel)
	if err != nil {
		return false
	}
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// Read returns the file at rel.
func (s *Store) Read(rel string) ([]byte, error) {
	p, err := s.path(rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

// Remove deletes files, ignoring blanks and the ones that are already gone.
func (s *Store) Remove(rels ...string) error {
	var errs []error
	for _, rel := range rels {
		if rel == "" {
			continue
		}
		p, err := s.path(rel)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	return firstErr(errs...)
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// Image is an image file that has been looked at: what it is and the web
// sized copies made from it.
type Image struct {
	Format        string // "jpeg", "png", "gif", "webp"
	Ext           string // with the dot
	Mime          string
	Width, Height int // as displayed, with the camera's orientation applied

	Large []byte // a web sized jpeg; nil when the original is small enough to use
	Thumb []byte // a jpeg; nil if the image couldn't be decoded
}

var formats = map[string]struct{ ext, mime string }{
	"jpeg": {".jpg", "image/jpeg"},
	"png":  {".png", "image/png"},
	"gif":  {".gif", "image/gif"},
	"webp": {".webp", "image/webp"},
}

// LookAt works out what data is, and makes the copies of it that the site
// serves. orientation is the EXIF orientation of the file (1 if none).
func LookAt(data []byte, orientation int) (*Image, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	f, ok := formats[format]
	if !ok {
		return nil, fmt.Errorf("unsupported image format %q", format)
	}
	img := &Image{Format: format, Ext: f.ext, Mime: f.mime}
	img.Width, img.Height = orientedSize(cfg.Width, cfg.Height, orientation)

	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		// we can serve the original, we just can't make anything smaller of it
		return img, nil
	}

	// shrink first, so that the orientation work is done on few pixels
	if max(cfg.Width, cfg.Height) > largeEdge {
		large := orient(shrink(src, largeEdge), orientation)
		if img.Large, err = encodeJPEG(large); err != nil {
			return nil, err
		}
		src = large
	} else {
		src = orient(src, orientation)
	}
	if img.Thumb, err = encodeJPEG(shrink(src, thumbEdge)); err != nil {
		return nil, err
	}
	return img, nil
}

// shrink scales img down so its long edge is at most edge; it is never scaled up.
func shrink(img image.Image, edge int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if max(w, h) <= edge {
		return img
	}
	if w >= h {
		h = max(1, h*edge/w)
		w = edge
	} else {
		w = max(1, w*edge/h)
		h = edge
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
	return dst
}

// orient turns img so that it is shown the right way up, given its EXIF
// orientation (the numbers are those of the spec).
func orient(img image.Image, o int) image.Image {
	if o < 2 || o > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var nx, ny int
			switch o {
			case 2: // mirrored
				nx, ny = w-1-x, y
			case 3: // upside down
				nx, ny = w-1-x, h-1-y
			case 4: // flipped
				nx, ny = x, h-1-y
			case 5: // transposed
				nx, ny = y, x
			case 6: // a quarter turn clockwise
				nx, ny = h-1-y, x
			case 7: // transversed
				nx, ny = h-1-y, w-1-x
			case 8: // a quarter turn counterclockwise
				nx, ny = y, w-1-x
			}
			dst.Set(nx, ny, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// encodeJPEG encodes img; anything see-through is put on white first.
func encodeJPEG(img image.Image) ([]byte, error) {
	b := img.Bounds()
	flat := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(flat, flat.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), img, b.Min, draw.Over)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, flat, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

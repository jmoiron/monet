package gallery

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/rwcarlsen/goexif/exif"
	"github.com/rwcarlsen/goexif/tiff"
)

// Exif is what could be read from the metadata in an image file. Zero values
// (and nil coordinates) mean the file didn't say.
type Exif struct {
	Make, Model, Lens, Software string
	FocalLength, Aperture       float64
	ISO                         int
	Exposure                    string
	Taken                       time.Time // the wall clock, in UTC
	Latitude, Longitude         *float64
	Altitude                    *float64
	Orientation                 int // 1-8, as in the spec; 1 when unknown
}

const (
	tagLensModel = 0xA434
	tagLensMake  = 0xA433
)

// ReadExif reads the EXIF block of a JPEG (or TIFF) file. Files with no EXIF,
// which is most of what the web serves, and formats it doesn't read (PNG, WebP,
// HEIC) yield an empty Exif and no error.
func ReadExif(data []byte) Exif {
	e := Exif{Orientation: 1}
	x, err := exif.Decode(bytes.NewReader(data))
	if err != nil || x == nil {
		return e
	}

	e.Make = str(x, exif.Make)
	e.Model = str(x, exif.Model)
	e.Software = str(x, exif.Software)

	if t, err := x.Get(exif.FocalLength); err == nil {
		e.FocalLength = ratFloat(t)
	}
	if t, err := x.Get(exif.FNumber); err == nil {
		e.Aperture = ratFloat(t)
	}
	if t, err := x.Get(exif.ISOSpeedRatings); err == nil {
		if v, err := t.Int(0); err == nil {
			e.ISO = v
		}
	}
	if t, err := x.Get(exif.ExposureTime); err == nil {
		e.Exposure = exposure(t)
	}
	if t, err := x.Get(exif.Orientation); err == nil {
		if v, err := t.Int(0); err == nil && v >= 1 && v <= 8 {
			e.Orientation = v
		}
	}
	if s := str(x, exif.DateTimeOriginal); s != "" {
		e.Taken = ParseExifTime(s)
	} else if s := str(x, exif.DateTime); s != "" {
		e.Taken = ParseExifTime(s)
	}

	// the lens isn't one of the tags goexif names
	x.Walk(walker(func(tag *tiff.Tag) {
		switch tag.Id {
		case tagLensModel:
			if s, err := tag.StringVal(); err == nil && e.Lens == "" {
				e.Lens = strings.TrimSpace(s)
			}
		}
	}))

	if lat, lon, err := x.LatLong(); err == nil && !(lat == 0 && lon == 0) {
		e.Latitude, e.Longitude = &lat, &lon
		if t, err := x.Get(exif.GPSAltitude); err == nil {
			alt := ratFloat(t)
			// 1 in the altitude ref means below sea level
			if r, err := x.Get(exif.GPSAltitudeRef); err == nil {
				if v, err := r.Int(0); err == nil && v == 1 {
					alt = -alt
				}
			}
			e.Altitude = &alt
		}
	}
	return e
}

type walker func(*tiff.Tag)

func (w walker) Walk(_ exif.FieldName, tag *tiff.Tag) error {
	w(tag)
	return nil
}

func str(x *exif.Exif, name exif.FieldName) string {
	t, err := x.Get(name)
	if err != nil {
		return ""
	}
	s, err := t.StringVal()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.Trim(s, "\x00"))
}

func ratFloat(t *tiff.Tag) float64 {
	n, d, err := t.Rat2(0)
	if err != nil || d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

// exposure is a shutter speed as a photographer writes it: "1/250", or "2" for
// two whole seconds.
func exposure(t *tiff.Tag) string {
	n, d, err := t.Rat2(0)
	if err != nil || n == 0 || d == 0 {
		return ""
	}
	if n >= d {
		return trimFloat(float64(n) / float64(d))
	}
	// 10/2500 is 1/250
	if n != 1 && d%n == 0 {
		return fmt.Sprintf("1/%d", d/n)
	}
	return fmt.Sprintf("1/%d", (d+n/2)/n)
}

// ParseExifTime parses the "2006:01:02 15:04:05" EXIF uses for dates. There's
// no zone, so the wall clock is returned as if it were UTC. Zero if it doesn't
// parse, including the all-zero date some cameras write for "unknown".
func ParseExifTime(s string) time.Time {
	s = strings.TrimSpace(strings.Trim(s, "\x00"))
	t, err := time.Parse("2006:01:02 15:04:05", s)
	if err != nil || t.Year() < 1900 {
		return time.Time{}
	}
	return t
}

// orientedSize is the width and height a viewer shows, given the file's
// stored size and EXIF orientation: 5-8 are rotated a quarter turn.
func orientedSize(w, h, orientation int) (int, int) {
	if orientation >= 5 && orientation <= 8 {
		return h, w
	}
	return w, h
}

package gallery

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestReadExif(t *testing.T) {
	data, err := os.ReadFile("testdata/exif.jpg")
	assert.NoError(t, err)

	e := ReadExif(data)
	assert.Equal(t, "TestCam", e.Make)
	assert.Equal(t, "TC-1 Mark II", e.Model)
	assert.Equal(t, "Test 35mm f/1.4 ASPH", e.Lens)
	assert.Equal(t, 35.0, e.FocalLength)
	assert.Equal(t, 1.4, e.Aperture)
	assert.Equal(t, 400, e.ISO)
	assert.Equal(t, "1/250", e.Exposure)
	assert.Equal(t, 6, e.Orientation)
	assert.Equal(t, time.Date(2024, 7, 4, 18, 30, 15, 0, time.UTC), e.Taken)
	if assert.NotNil(t, e.Latitude) && assert.NotNil(t, e.Longitude) {
		assert.InDelta(t, 40.6892, *e.Latitude, 0.0001)
		assert.InDelta(t, -74.0445, *e.Longitude, 0.0001)
	}
	if assert.NotNil(t, e.Altitude) {
		assert.InDelta(t, 12, *e.Altitude, 0.01)
	}
}

func TestReadExifAbsent(t *testing.T) {
	// not an image at all, and an image with no exif: both are just empty
	for _, data := range [][]byte{nil, []byte("not an image"), tinyPNG(t)} {
		e := ReadExif(data)
		assert.Equal(t, Exif{Orientation: 1}, e)
	}
}

func TestParseExifTime(t *testing.T) {
	assert.Equal(t, time.Date(2026, 2, 12, 16, 45, 12, 0, time.UTC), ParseExifTime("2026:02:12 16:45:12"))
	assert.True(t, ParseExifTime("0000:00:00 00:00:00").IsZero())
	assert.True(t, ParseExifTime("garbage").IsZero())
	assert.True(t, ParseExifTime("").IsZero())
}

func TestOrientedSize(t *testing.T) {
	w, h := orientedSize(4000, 3000, 1)
	assert.Equal(t, [2]int{4000, 3000}, [2]int{w, h})
	w, h = orientedSize(4000, 3000, 6)
	assert.Equal(t, [2]int{3000, 4000}, [2]int{w, h})
}

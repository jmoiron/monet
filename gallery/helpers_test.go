package gallery

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestService(t *testing.T) *PhotoService {
	t.Helper()
	dbh, err := sqlx.Connect("sqlite3", filepath.Join(t.TempDir(), "gallery.db"))
	require.NoError(t, err)
	t.Cleanup(func() { dbh.Close() })

	m, err := monarch.NewManager(dbh)
	require.NoError(t, err)
	require.NoError(t, m.Upgrade(galleryMigrations))

	store, err := NewStore(filepath.Join(t.TempDir(), "photos"))
	require.NoError(t, err)
	return NewPhotoService(dbh, store)
}

func gradient(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 255 / w), uint8(y * 255 / h), 90, 255})
		}
	}
	return img
}

func testJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, gradient(w, h), nil))
	return buf.Bytes()
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, gradient(8, 6)))
	return buf.Bytes()
}

func imageSize(t *testing.T, data []byte) (int, int) {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	require.NoError(t, err)
	return cfg.Width, cfg.Height
}

func writeFile(t *testing.T, root, rel string, data []byte) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, data, 0o644))
}

// zipDir zips the files under dir, with every path under prefix (which may be "").
func zipDir(t *testing.T, dir, prefix string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "export.zip")
	f, err := os.Create(out)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	require.NoError(t, filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		w, err := zw.Create(prefix + filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	}))
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
	return out
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return b
}

func TestHelpersSane(t *testing.T) {
	w, h := imageSize(t, testJPEG(t, 30, 20))
	assert.Equal(t, [2]int{30, 20}, [2]int{w, h})
}

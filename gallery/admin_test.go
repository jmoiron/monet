package gallery

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/monet/pkg/vfs"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// an admin on a database opened the way monet opens its own (shared cache,
// where concurrent writers get "database table is locked")
func newTestAdmin(t *testing.T) (*Admin, http.Handler, *PhotoService) {
	t.Helper()
	dbh, err := sqlx.Connect("sqlite3", "file:"+filepath.Join(t.TempDir(), "gallery.db")+"?cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { dbh.Close() })
	m, err := monarch.NewManager(dbh)
	require.NoError(t, err)
	require.NoError(t, m.Upgrade(galleryMigrations))

	fss := vfs.NewRegistry(vfs.NewURLMapper(map[string]string{FSName: "/i/photos/"}))
	require.NoError(t, fss.AddPath(FSName, t.TempDir()))

	a := NewAdmin(dbh, fss)
	r := chi.NewRouter()
	a.Bind(r)
	svc, err := NewPhotoServiceFromFSS(dbh, fss)
	require.NoError(t, err)
	return a, r, svc
}

func multipartBody(t *testing.T, field, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if field != "" {
		fw, err := mw.CreateFormFile(field, filename)
		require.NoError(t, err)
		_, err = fw.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, mw.Close())
	return &buf, mw.FormDataContentType()
}

func post(t *testing.T, h http.Handler, url, field, filename string, data []byte) (int, map[string]any) {
	t.Helper()
	body, ctype := multipartBody(t, field, filename, data)
	req := httptest.NewRequest("POST", url, body)
	req.Header.Set("Content-Type", ctype)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
	return w.Code, out
}

func TestUploadPhoto(t *testing.T) {
	_, h, svc := newTestAdmin(t)
	data := testJPEG(t, 300, 200)

	code, out := post(t, h, "/gallery/upload/", "file", "a.jpg", data)
	assert.Equal(t, 200, code)
	assert.Equal(t, true, out["success"])
	id, _ := out["id"].(string)
	p, err := svc.Get(id)
	require.NoError(t, err)
	assert.Equal(t, SourceUpload, p.Source)
	assert.Equal(t, 0, p.Published)
	assert.True(t, svc.Store().Exists(p.File))

	// the same bytes again is not another photo
	code, out = post(t, h, "/gallery/upload/", "file", "again.jpg", data)
	assert.Equal(t, 200, code)
	assert.Equal(t, true, out["duplicate"])
	assert.Equal(t, id, out["id"])
	n, _ := svc.Count("")
	assert.Equal(t, 1, n)
}

func TestUploadPhotoErrorsSayWhy(t *testing.T) {
	a, h, svc := newTestAdmin(t)

	code, out := post(t, h, "/gallery/upload/", "file", "x.jpg", []byte("<html>not a photo"))
	assert.Equal(t, http.StatusUnprocessableEntity, code)
	assert.Equal(t, false, out["success"])
	assert.Contains(t, out["error"], "not an image")

	code, out = post(t, h, "/gallery/upload/", "", "", nil)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, out["error"], "no file")

	// too big: the limit is the admin's, and the answer names it
	a.maxPhoto = 1 << 10
	code, out = post(t, h, "/gallery/upload/", "file", "big.jpg", testJPEG(t, 600, 600))
	assert.Equal(t, http.StatusRequestEntityTooLarge, code)
	assert.Contains(t, out["error"], "too large")
	assert.Contains(t, out["error"], "1.0 kB")

	n, _ := svc.Count("")
	assert.Equal(t, 0, n, "none of those left anything behind")
}

func TestLimitsAreAtLeast256MB(t *testing.T) {
	assert.GreaterOrEqual(t, int64(MaxPhotoBytes), int64(256<<20))
	assert.GreaterOrEqual(t, int64(MaxInstagramBytes), int64(256<<20))
	a, _, _ := newTestAdmin(t)
	assert.Equal(t, int64(MaxPhotoBytes), a.maxPhoto)
	assert.Equal(t, int64(MaxInstagramBytes), a.maxInstagram)
}

// dropping a folder of photos sends them all at once
func TestUploadPhotosAtOnce(t *testing.T) {
	_, h, svc := newTestAdmin(t)

	const n = 12
	var wg sync.WaitGroup
	codes := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _ = post(t, h, "/gallery/upload/", "file", "p.jpg", testJPEG(t, 100+i, 80))
		}()
	}
	wg.Wait()

	for i, c := range codes {
		assert.Equal(t, 200, c, "upload %d", i)
	}
	got, _ := svc.Count("")
	assert.Equal(t, n, got)
}

func TestImportInstagramUpload(t *testing.T) {
	a, h, svc := newTestAdmin(t)
	zipPath := zipDir(t, firstExport(t).write(), "")
	zipData := readFile(t, zipPath)

	code, out := post(t, h, "/gallery/insta/", "file", "export.zip", zipData)
	assert.Equal(t, 200, code)
	assert.Equal(t, true, out["success"])
	rep := out["report"].(map[string]any)
	assert.EqualValues(t, 4, rep["created"])
	assert.EqualValues(t, 1, rep["videos"])
	assert.Equal(t, []any{"media/other/1006.jpg"}, rep["missing"])
	assert.Contains(t, out["message"], "4 new")
	n, _ := svc.Count("")
	assert.Equal(t, 4, n)

	// the same export again adds nothing
	code, out = post(t, h, "/gallery/insta/", "file", "export.zip", zipData)
	assert.Equal(t, 200, code)
	rep = out["report"].(map[string]any)
	assert.EqualValues(t, 0, rep["created"])
	assert.EqualValues(t, 4, rep["unchanged"])

	// not a zip, nothing at all, and too big
	code, out = post(t, h, "/gallery/insta/", "file", "x.zip", []byte("not a zip"))
	assert.Equal(t, http.StatusUnprocessableEntity, code)
	assert.Equal(t, false, out["success"])
	code, out = post(t, h, "/gallery/insta/", "", "", nil)
	assert.Equal(t, http.StatusBadRequest, code)
	a.maxInstagram = 1 << 10
	code, out = post(t, h, "/gallery/insta/", "file", "export.zip", zipData)
	assert.Equal(t, http.StatusRequestEntityTooLarge, code)
	assert.Contains(t, out["error"], "too large")
}

func TestWritesTakeTurns(t *testing.T) {
	a, _, _ := newTestAdmin(t)
	release, err := a.acquire(context.Background())
	require.NoError(t, err)

	// a second one waits, and gives up if its request goes away
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = a.acquire(ctx)
	assert.ErrorIs(t, err, context.Canceled)

	release()
	release2, err := a.acquire(context.Background())
	assert.NoError(t, err)
	release2()
}

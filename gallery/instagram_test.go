package gallery

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// an export on disk, built up the way instagram's is
type exportBuilder struct {
	t        *testing.T
	dir      string
	posts    []map[string]any
	insights []map[string]any
}

func newExport(t *testing.T) *exportBuilder {
	return &exportBuilder{t: t, dir: t.TempDir()}
}

func (b *exportBuilder) media(uri, title string, ts int64, exif ...map[string]any) map[string]any {
	m := map[string]any{
		"uri": uri, "creation_timestamp": ts, "title": title,
		"cross_post_source": map[string]any{"source_app": "FB"},
		"media_metadata":    map[string]any{"camera_metadata": map[string]any{"has_camera_metadata": false}},
	}
	if len(exif) > 0 {
		m["media_metadata"].(map[string]any)["photo_metadata"] = map[string]any{"exif_data": exif}
	}
	return m
}

// file writes the file a post names, as a jpeg of the given width
func (b *exportBuilder) file(uri string, w int) {
	writeFile(b.t, b.dir, uri, testJPEG(b.t, w, 30))
}

func (b *exportBuilder) likes(uri string, likes, comments int) {
	b.insights = append(b.insights, map[string]any{
		"title":          "",
		"media_map_data": map[string]any{"Media Thumbnail": map[string]any{"uri": uri}},
		"string_map_data": map[string]any{
			"Likes":            map[string]any{"value": strconv.Itoa(likes)},
			"Comments":         map[string]any{"value": strconv.Itoa(comments)},
			"Saves":            map[string]any{"value": "2"},
			"Accounts reached": map[string]any{"value": "90"},
			"Impressions":      map[string]any{"value": "96"},
		},
	})
}

func (b *exportBuilder) write() string {
	b.t.Helper()
	posts, _ := json.Marshal(b.posts)
	writeFile(b.t, b.dir, "your_instagram_activity/media/posts_1.json", posts)
	ins, _ := json.Marshal(map[string]any{"organic_insights_posts": b.insights})
	writeFile(b.t, b.dir, "logged_information/past_instagram_insights/posts.json", ins)
	return b.dir
}

// a first export: a photo with exif, a carousel, a video, one file that isn't there
func firstExport(t *testing.T) *exportBuilder {
	b := newExport(t)
	gps := map[string]any{"latitude": 32.64, "longitude": 128.85}
	when := map[string]any{"date_time_original": "2026:02:12 16:45:12", "software": "HDR+ 1.0", "device_id": "android-1"}
	b.posts = []map[string]any{
		{"media": []any{b.media("media/other/1001.jpg", "Retreat", 1770882831, gps, when)}},
		// "Côte d'Azur" the way the export writes it: each utf-8 byte its own character
		{"media": []any{b.media("media/other/1002.webp", "CÃ´te d'Azur", 1770000000)}},
		{"title": "Two of them", "creation_timestamp": 1760000000, "media": []any{
			b.media("media/posts/202510/1003.jpg", "", 1760000000),
			b.media("media/posts/202510/1004.jpg", "", 1760000000),
		}},
		{"media": []any{b.media("media/reels/1005.mp4", "A reel", 1750000000)}},
		{"media": []any{b.media("media/other/1006.jpg", "Gone", 1740000000)}},
	}
	b.file("media/other/1001.jpg", 40)
	writeFile(t, b.dir, "media/other/1002.webp", testJPEG(t, 50, 30)) // not really webp: see below
	b.file("media/posts/202510/1003.jpg", 60)
	b.file("media/posts/202510/1004.jpg", 70)
	b.file("media/reels/1005.mp4", 10)
	b.likes("media/other/1001.jpg", 12, 1)
	b.likes("media/posts/202510/1003.jpg", 34, 0)
	return b
}

func TestInstagramImport(t *testing.T) {
	svc := newTestService(t)
	b := firstExport(t)
	// the "webp" above is a jpeg by another name; the importer goes by what's in the file
	rep, err := NewInstagramImporter(svc).ImportPath(context.Background(), b.write())
	require.NoError(t, err)

	assert.Equal(t, 5, rep.Posts)
	assert.Equal(t, 5, rep.Media, "the video isn't counted")
	assert.Equal(t, 4, rep.Created)
	assert.Equal(t, 1, rep.Videos)
	assert.Equal(t, []string{"media/other/1006.jpg"}, rep.Missing)
	assert.Empty(t, rep.Failed)
	t.Log(rep)

	p, err := svc.Find(SourceInstagram, "1001")
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, uuid.NewSHA1(instagramNS, []byte("1001")).String(), p.ID, "ids are the same wherever it's imported")
	assert.Equal(t, "Retreat", p.Caption)
	assert.Equal(t, 1, p.Published, "public posts are shown")
	assert.Equal(t, 12, p.Likes)
	assert.Equal(t, 1, p.Comments)
	assert.Equal(t, int64(1770882831), p.PostedAt)
	assert.Equal(t, time.Date(2026, 2, 12, 16, 45, 12, 0, time.UTC), p.Taken(), "the time taken is from the json, as the file has no exif")
	if assert.True(t, p.HasGPS()) {
		assert.Equal(t, 32.64, *p.Latitude)
	}
	assert.Equal(t, "HDR+ 1.0", p.Software)
	assert.Equal(t, "40 × 30", p.Resolution())
	assert.True(t, svc.Store().Exists(p.File))
	assert.True(t, svc.Store().Exists(p.ThumbFile()))
	if m := p.Meta().Instagram; assert.NotNil(t, m) {
		assert.Equal(t, 2, m.Saves)
		assert.Equal(t, 90, m.Reach)
		assert.Equal(t, "android-1", m.DeviceID)
		assert.Equal(t, "FB", m.CrossPost)
		assert.Equal(t, "media/other/1001.jpg", m.URI)
	}

	// a post with no insights has no counts, and its time taken is when it was posted
	p, _ = svc.Find(SourceInstagram, "1002")
	require.NotNil(t, p)
	assert.Equal(t, "Côte d'Azur", p.Caption, "the text is repaired")
	assert.Equal(t, 0, p.Likes)
	assert.Equal(t, p.PostedAt, p.TakenAt)
	assert.False(t, p.HasGPS())

	// a carousel's photos are grouped, in order, with the post's own caption
	p3, _ := svc.Find(SourceInstagram, "1003")
	p4, _ := svc.Find(SourceInstagram, "1004")
	require.NotNil(t, p3)
	require.NotNil(t, p4)
	assert.Equal(t, "post:1003", p3.SourceGroup)
	assert.Equal(t, p3.SourceGroup, p4.SourceGroup)
	assert.Equal(t, "Two of them", p3.Caption)
	assert.Equal(t, 1, p3.Meta().Instagram.Index)
	assert.Equal(t, 2, p4.Meta().Instagram.Index)
	assert.Equal(t, 34, p3.Likes)
}

func TestInstagramImportIsIdempotent(t *testing.T) {
	svc := newTestService(t)
	im := NewInstagramImporter(svc)
	dir := firstExport(t).write()

	first, err := im.ImportPath(context.Background(), dir)
	require.NoError(t, err)
	require.Equal(t, 4, first.Created)

	var before []string
	photos, _ := svc.Select("ORDER BY id")
	for _, p := range photos {
		before = append(before, p.ID+p.SHA256+p.File)
	}

	again, err := im.ImportPath(context.Background(), dir)
	require.NoError(t, err)
	assert.Equal(t, 0, again.Created)
	assert.Equal(t, 0, again.Updated)
	assert.Equal(t, 4, again.Unchanged)

	photos, _ = svc.Select("ORDER BY id")
	var after []string
	for _, p := range photos {
		after = append(after, p.ID+p.SHA256+p.File)
	}
	assert.Equal(t, before, after, "same photos, same files")
}

func TestInstagramImportOfALaterExport(t *testing.T) {
	svc := newTestService(t)
	im := NewInstagramImporter(svc)
	_, err := im.ImportPath(context.Background(), firstExport(t).write())
	require.NoError(t, err)

	// I've fixed the caption of one, and taken another down
	p, _ := svc.Find(SourceInstagram, "1001")
	p.Caption = "Retreat (my words)"
	require.NoError(t, svc.Save(p))
	p, _ = svc.Find(SourceInstagram, "1002")
	p.Published = 0
	require.NoError(t, svc.Save(p))

	// then the next export: more likes on one, a new post, the missing file turned up
	b := firstExport(t)
	b.insights = nil
	b.likes("media/other/1001.jpg", 15, 2)
	b.likes("media/posts/202510/1003.jpg", 34, 0)
	b.posts = append([]map[string]any{{"media": []any{b.media("media/posts/202603/1007.jpg", "New one", 1774000000)}}}, b.posts...)
	b.file("media/posts/202603/1007.jpg", 25)
	b.file("media/other/1006.jpg", 35)
	rep, err := im.ImportPath(context.Background(), b.write())
	require.NoError(t, err)

	assert.Equal(t, 2, rep.Created, "the new post, and the one that was missing")
	assert.Equal(t, 1, rep.Updated, "the one with more likes")
	assert.Equal(t, 3, rep.Unchanged)
	assert.Empty(t, rep.Missing)

	p, _ = svc.Find(SourceInstagram, "1001")
	assert.Equal(t, 15, p.Likes)
	assert.Equal(t, 2, p.Comments)
	assert.Equal(t, "Retreat (my words)", p.Caption, "edits made here are kept")
	p, _ = svc.Find(SourceInstagram, "1002")
	assert.Equal(t, 0, p.Published, "and so is what I've taken down")

	n, _ := svc.Count("")
	assert.Equal(t, 6, n)
}

func TestInstagramImportFromZip(t *testing.T) {
	dir := firstExport(t).write()

	// directly in the zip, as instagram makes it, and in a folder in it
	for _, prefix := range []string{"", "instagram-someone-2026-03-23/"} {
		svc := newTestService(t)
		rep, err := NewInstagramImporter(svc).ImportPath(context.Background(), zipDir(t, dir, prefix))
		require.NoError(t, err, prefix)
		assert.Equal(t, 4, rep.Created, prefix)
	}

	// and an unzipped one in a folder
	parent := t.TempDir()
	require.NoError(t, os.Rename(dir, filepath.Join(parent, "export")))
	rep, err := NewInstagramImporter(newTestService(t)).ImportPath(context.Background(), parent)
	require.NoError(t, err)
	assert.Equal(t, 4, rep.Created)
}

func TestInstagramImportOfSomethingElse(t *testing.T) {
	svc := newTestService(t)
	_, err := NewInstagramImporter(svc).ImportPath(context.Background(), t.TempDir())
	assert.ErrorContains(t, err, "doesn't look like an instagram export")
	_, err = NewInstagramImporter(svc).ImportPath(context.Background(), filepath.Join(t.TempDir(), "nope.zip"))
	assert.Error(t, err)
}

func TestInstagramImportIgnoresPathsOutsideTheExport(t *testing.T) {
	svc := newTestService(t)
	b := newExport(t)
	b.posts = []map[string]any{{"media": []any{b.media("../../etc/passwd.jpg", "bad", 1)}}}
	rep, err := NewInstagramImporter(svc).ImportPath(context.Background(), b.write())
	require.NoError(t, err)
	assert.Equal(t, 0, rep.Created)
	assert.Len(t, rep.Failed, 1)
}

func TestInstagramImportCanBeCancelled(t *testing.T) {
	svc := newTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewInstagramImporter(svc).ImportPath(ctx, firstExport(t).write())
	assert.ErrorIs(t, err, context.Canceled)
	n, _ := svc.Count("")
	assert.Equal(t, 0, n)
}

func TestFixMojibake(t *testing.T) {
	for in, want := range map[string]string{
		"CÃ´te d'Azur":         "Côte d'Azur",
		"â\u009c\u0085 done":   "✅ done", // a 3 byte character
		"plain":                "plain",
		"":                     "",
		"Côte (already right)": "Côte (already right)",
		"お休み":                  "お休み",
		"cafÃ© Ã\u00a0 Ã©":     "café à é",
	} {
		assert.Equal(t, want, fixMojibake(in), in)
	}
}

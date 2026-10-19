package gallery

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddReadsTheFile(t *testing.T) {
	svc := newTestService(t)
	data, err := os.ReadFile("testdata/exif.jpg")
	require.NoError(t, err)

	p, err := svc.Add(NewPhoto{Source: SourceUpload, Data: data, Caption: "a test"})
	require.NoError(t, err)

	got, err := svc.Get(p.ID)
	require.NoError(t, err)
	assert.Equal(t, "image/jpeg", got.Mime)
	assert.Equal(t, int64(len(data)), got.Bytes)
	assert.Equal(t, "TestCam TC-1 Mark II", got.Camera())
	assert.Equal(t, "Test 35mm f/1.4 ASPH", got.LensModel)
	assert.Equal(t, "35mm · f/1.4 · 1/250s · ISO 400", got.ExposureSummary())
	assert.Equal(t, time.Date(2024, 7, 4, 18, 30, 15, 0, time.UTC), got.Taken())
	assert.True(t, got.HasGPS())
	assert.InDelta(t, 40.6892, *got.Latitude, 0.0001)
	assert.Equal(t, "a test", got.Caption)
	assert.Equal(t, 0, got.Published, "uploads aren't shown until asked")

	// stored 64x48 and turned a quarter by its orientation
	assert.Equal(t, "48 × 64", got.Resolution())
	assert.NotEmpty(t, got.SHA256)

	for _, f := range []string{got.File, got.ThumbFile(), got.DisplayFile()} {
		assert.True(t, svc.Store().Exists(f), f)
	}

	found, err := svc.FindBySHA(SourceUpload, got.SHA256)
	require.NoError(t, err)
	if assert.NotNil(t, found) {
		assert.Equal(t, got.ID, found.ID)
	}
	other, err := svc.FindBySHA(SourceInstagram, got.SHA256)
	require.NoError(t, err)
	assert.Nil(t, other)
}

func TestAddWithoutExifTakesWhatTheSourceSays(t *testing.T) {
	svc := newTestService(t)
	lat, lon := 35.0, 139.0
	p, err := svc.Add(NewPhoto{
		Source: SourceInstagram, SourceID: "123", Data: testJPEG(t, 40, 30),
		PostedAt: 1700000000, TakenAt: 1699990000, Latitude: &lat, Longitude: &lon,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1699990000), p.TakenAt)
	assert.Equal(t, int64(1700000000), p.PostedAt)
	assert.Equal(t, 35.0, *p.Latitude)
	assert.Empty(t, p.Camera())
	assert.Empty(t, p.ExposureSummary())

	// with no time taken either, it was taken when it was posted
	p, err = svc.Add(NewPhoto{Source: SourceInstagram, SourceID: "124", Data: testJPEG(t, 40, 30), PostedAt: 1700000000})
	require.NoError(t, err)
	assert.Equal(t, int64(1700000000), p.TakenAt)
	assert.Nil(t, p.Latitude)
}

func TestAddRejectsWhatIsNotAnImage(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Add(NewPhoto{Source: SourceUpload, Data: []byte("<html>")})
	assert.ErrorIs(t, err, ErrNotAnImage)
	n, _ := svc.Count("")
	assert.Equal(t, 0, n)
}

func TestSourceIDIsUnique(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Add(NewPhoto{Source: SourceInstagram, SourceID: "1", Data: testJPEG(t, 20, 20)})
	require.NoError(t, err)

	// a second photo can't claim the same place at its source...
	_, err = svc.Add(NewPhoto{Source: SourceInstagram, SourceID: "1", Data: testJPEG(t, 21, 20)})
	assert.Error(t, err)
	// ...and the failed one left nothing behind
	n, _ := svc.Count("")
	assert.Equal(t, 1, n)

	// uploads have no source id, and there can be any number of them
	for i := 0; i < 2; i++ {
		_, err = svc.Add(NewPhoto{Source: SourceUpload, Data: testJPEG(t, 20, 20)})
		assert.NoError(t, err)
	}
}

func TestSaveAndDelete(t *testing.T) {
	svc := newTestService(t)
	p, err := svc.Add(NewPhoto{Source: SourceUpload, Data: testJPEG(t, 2400, 1600)})
	require.NoError(t, err)
	files := []string{p.File, p.Large, p.Thumb}
	for _, f := range files {
		require.True(t, svc.Store().Exists(f), f)
	}

	p.Title, p.Published = "Edited", 1
	require.NoError(t, svc.Save(p))
	got, err := svc.Get(p.ID)
	require.NoError(t, err)
	assert.Equal(t, "Edited", got.Title)
	assert.Equal(t, 1, got.Published)

	require.NoError(t, svc.Delete(p.ID))
	_, err = svc.Get(p.ID)
	assert.Error(t, err)
	for _, f := range files {
		assert.False(t, svc.Store().Exists(f), "%s is gone with the photo", f)
	}
}

func TestMetaRoundTrips(t *testing.T) {
	svc := newTestService(t)
	p, err := svc.Add(NewPhoto{
		Source: SourceInstagram, SourceID: "9", Data: testJPEG(t, 20, 20),
		Meta: Meta{Instagram: &InstagramMeta{Saves: 3, Reach: 90}},
	})
	require.NoError(t, err)
	got, _ := svc.Get(p.ID)
	if assert.NotNil(t, got.Meta().Instagram) {
		assert.Equal(t, 3, got.Meta().Instagram.Saves)
		assert.Equal(t, 90, got.Meta().Instagram.Reach)
	}
	assert.Nil(t, (&Photo{}).Meta().Instagram)
}

func TestAlbums(t *testing.T) {
	svc := newTestService(t)
	albums := NewAlbumService(svc.db)

	var ids []string
	for i := 0; i < 3; i++ {
		p, err := svc.Add(NewPhoto{Source: SourceUpload, Data: testJPEG(t, 20+i, 20), Published: i != 1, TakenAt: int64(100 - i)})
		require.NoError(t, err)
		ids = append(ids, p.ID)
	}

	a, err := albums.Create("Japan, 2026!", "trip")
	require.NoError(t, err)
	assert.Equal(t, "japan-2026", a.Slug)
	b, err := albums.Create("Japan, 2026!", "again")
	require.NoError(t, err)
	assert.Equal(t, "japan-2026-2", b.Slug, "slugs are unique")
	_, err = albums.Create("  ", "")
	assert.Error(t, err)

	// order is the order added, and adding twice doesn't add twice
	require.NoError(t, albums.Add(a.ID, ids[2], ids[0]))
	require.NoError(t, albums.Add(a.ID, ids[0], ids[1], "no-such-photo"))
	got, err := albums.Photos(a.ID, false)
	require.NoError(t, err)
	var order []string
	for _, p := range got {
		order = append(order, p.ID)
	}
	assert.Equal(t, []string{ids[2], ids[0], ids[1]}, order)

	shown, err := albums.Photos(a.ID, true)
	require.NoError(t, err)
	assert.Len(t, shown, 2, "unpublished photos are left out of public views")

	of, err := albums.AlbumsOf(ids[0], false)
	require.NoError(t, err)
	assert.Len(t, of, 1)

	require.NoError(t, albums.Remove(a.ID, ids[0]))
	got, _ = albums.Photos(a.ID, false)
	assert.Len(t, got, 2)

	// deleting a photo takes it out of its albums, and off a cover
	a.CoverID = ids[2]
	require.NoError(t, albums.Save(a))
	require.NoError(t, svc.Delete(ids[2]))
	got, _ = albums.Photos(a.ID, false)
	assert.Len(t, got, 1)
	a, _ = albums.Get(a.ID)
	assert.Empty(t, a.CoverID)

	// deleting an album keeps its photos
	require.NoError(t, albums.Delete(a.ID))
	n, _ := svc.Count("")
	assert.Equal(t, 2, n)
	list, _ := albums.List(false)
	assert.Len(t, list, 1)
}

func TestOrderNewest(t *testing.T) {
	svc := newTestService(t)
	add := func(name string, taken, posted int64) {
		_, err := svc.Add(NewPhoto{Source: SourceInstagram, SourceID: name, Data: testJPEG(t, 20, 20),
			TakenAt: taken, PostedAt: posted})
		require.NoError(t, err)
	}
	add("old", 1000, 5000) // taken long before it was posted
	add("new", 3000, 3500)
	add("posted-only", 0, 2000)
	add("middle", 2500, 0)
	// nothing known about when: it's from when it was added (now), so it's the newest
	_, err := svc.Add(NewPhoto{Source: SourceUpload, Data: testJPEG(t, 21, 20)})
	require.NoError(t, err)

	photos, err := svc.Select(OrderNewest)
	require.NoError(t, err)
	var got []string
	for _, p := range photos {
		got = append(got, p.SourceID)
	}
	assert.Equal(t, []string{"", "new", "middle", "posted-only", "old"}, got)

	// and it composes with where and limit, as the pages use it
	shown, err := svc.Select("WHERE source=? "+OrderNewest+" LIMIT ? OFFSET ?", SourceInstagram, 2, 1)
	require.NoError(t, err)
	require.Len(t, shown, 2)
	assert.Equal(t, "middle", shown[0].SourceID)
	assert.Equal(t, "posted-only", shown[1].SourceID)
}

func TestPhotoFactsMarksInstagram(t *testing.T) {
	p := &Photo{Source: SourceInstagram, PostedAt: 1700000000, Likes: 1, Comments: 2}
	var posted string
	for _, f := range photoFacts(p) {
		if f.Label == "Posted" {
			posted = string(f.Value)
		}
	}
	assert.Contains(t, posted, `fa-instagram`)
	assert.Contains(t, posted, "Nov 14, 2023 · 1 like · 2 comments")
	assert.NotContains(t, posted, "Instagram,", "the icon replaces the word")

	for _, f := range photoFacts(&Photo{Source: SourceUpload}) {
		assert.NotEqual(t, "Posted", f.Label, "uploads weren't posted anywhere")
	}
}

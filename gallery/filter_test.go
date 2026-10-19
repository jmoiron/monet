package gallery

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func day(y int, m time.Month, d, hour int) int64 {
	return time.Date(y, m, d, hour, 0, 0, 0, time.UTC).Unix()
}

// photos from a few sources and days; names are what to look for in results
func seedFilterPhotos(t *testing.T, svc *PhotoService) {
	t.Helper()
	f := func(v float64) *float64 { return &v }
	n := 20
	add := func(name, source string, taken, posted int64, lat, lng *float64) {
		n++
		_, err := svc.Add(NewPhoto{Source: source, SourceID: name, Title: name, Data: testJPEG(t, n, 20),
			TakenAt: taken, PostedAt: posted, Latitude: lat, Longitude: lng, Published: true})
		require.NoError(t, err)
	}
	add("a2022", SourceInstagram, day(2022, 12, 31, 23), 0, f(10), f(20))
	add("b2023-start", SourceUpload, day(2023, 1, 1, 0), 0, nil, nil)
	add("c2023-mid", SourceInstagram, day(2023, 6, 15, 12), 0, f(48.85), f(2.35))
	add("d2023-end", SourceUpload, day(2023, 12, 31, 23), 0, f(35.6), f(139.7))
	add("e2024-posted", SourceInstagram, 0, day(2024, 3, 3, 9), nil, nil)
}

func titles(photos []Photo) []string {
	var out []string
	for _, p := range photos {
		out = append(out, p.Title)
	}
	return out
}

func filtered(t *testing.T, svc *PhotoService, query string) []string {
	t.Helper()
	q, err := url.ParseQuery(query)
	require.NoError(t, err)
	where, args := ParseFilter(q).Where()
	photos, err := svc.Select(where+OrderNewest, args...)
	require.NoError(t, err)
	return titles(photos)
}

func TestFilter(t *testing.T) {
	svc := newTestService(t)
	seedFilterPhotos(t, svc)

	tests := map[string]struct {
		query string
		want  []string
	}{
		"nothing":             {"", []string{"e2024-posted", "d2023-end", "c2023-mid", "b2023-start", "a2022"}},
		"source":              {"source=upload", []string{"d2023-end", "b2023-start"}},
		"unknown source":      {"source=flickr", []string{"e2024-posted", "d2023-end", "c2023-mid", "b2023-start", "a2022"}},
		"year":                {"year=2023", []string{"d2023-end", "c2023-mid", "b2023-start"}},
		"year uses posted":    {"year=2024", []string{"e2024-posted"}},
		"days, both included": {"from=2023-01-01&to=2023-12-31", []string{"d2023-end", "c2023-mid", "b2023-start"}},
		"from only":           {"from=2023-06-15", []string{"e2024-posted", "d2023-end", "c2023-mid"}},
		"to only":             {"to=2023-01-01", []string{"b2023-start", "a2022"}},
		"a single day":        {"from=2023-06-15&to=2023-06-15", []string{"c2023-mid"}},
		"days beat year":      {"year=2022&from=2023-06-01&to=2023-06-30", []string{"c2023-mid"}},
		"has gps":             {"gps=has", []string{"d2023-end", "c2023-mid", "a2022"}},
		"lacks gps":           {"gps=lacks", []string{"e2024-posted", "b2023-start"}},
		"unknown gps":         {"gps=maybe", []string{"e2024-posted", "d2023-end", "c2023-mid", "b2023-start", "a2022"}},
		"combined":            {"source=instagram&year=2023&gps=has", []string{"c2023-mid"}},
		"junk is ignored":     {"year=abc&from=yesterday&to=", []string{"e2024-posted", "d2023-end", "c2023-mid", "b2023-start", "a2022"}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, filtered(t, svc, tc.query))
		})
	}
}

func TestFilterRoundTrips(t *testing.T) {
	for _, query := range []string{"", "source=upload", "year=2023", "from=2023-01-02&to=2023-02-03", "gps=lacks&source=instagram&collection=abc"} {
		q, _ := url.ParseQuery(query)
		f := ParseFilter(q)
		assert.Equal(t, f, ParseFilter(f.Values()), query)
	}
	assert.Equal(t, "", Filter{}.Query())
	assert.True(t, Filter{}.IsZero())
	assert.False(t, ParseFilter(url.Values{"source": {"upload"}}).IsZero())
}

func TestYears(t *testing.T) {
	svc := newTestService(t)
	seedFilterPhotos(t, svc)
	years, err := svc.Years()
	require.NoError(t, err)
	assert.Equal(t, []YearCount{{2024, 1}, {2023, 3}, {2022, 1}}, years)
}

func TestCollectionsOfPhotos(t *testing.T) {
	svc := newTestService(t)
	seedFilterPhotos(t, svc)
	albums := NewAlbumService(svc.db)
	all, err := svc.Select(OrderNewest)
	require.NoError(t, err)

	trip, err := albums.Create("Trip", "somewhere")
	require.NoError(t, err)
	require.NoError(t, albums.Add(trip.ID, all[0].ID, all[1].ID, all[2].ID))
	empty, err := albums.Create("Empty", "")
	require.NoError(t, err)

	// the filter finds the photos of a collection
	where, args := Filter{Collection: trip.ID}.Where()
	in, err := svc.Select(where+OrderNewest, args...)
	require.NoError(t, err)
	assert.Equal(t, []string{"e2024-posted", "d2023-end", "c2023-mid"}, titles(in))

	// the public sees only what is published, and only published photos in it
	sums, err := albums.Summaries(true)
	require.NoError(t, err)
	assert.Empty(t, sums)

	trip.Published = 1
	require.NoError(t, albums.Save(trip))
	hidden := all[0]
	hidden.Published = 0
	require.NoError(t, svc.Save(&hidden))

	sums, err = albums.Summaries(true)
	require.NoError(t, err)
	require.Len(t, sums, 1)
	assert.Equal(t, 2, sums[0].Count)
	assert.NotEmpty(t, sums[0].Cover())

	sums, err = albums.Summaries(false)
	require.NoError(t, err)
	require.Len(t, sums, 2)
	counts := map[string]int{}
	for _, s := range sums {
		counts[s.Title] = s.Count
	}
	assert.Equal(t, map[string]int{"Trip": 3, "Empty": 0}, counts)
	_ = empty

	// a photo's public page names only published collections
	pub, err := albums.AlbumsOf(all[1].ID, true)
	require.NoError(t, err)
	assert.Len(t, pub, 1)
	require.NoError(t, albums.Add(empty.ID, all[1].ID))
	pub, err = albums.AlbumsOf(all[1].ID, true)
	require.NoError(t, err)
	assert.Len(t, pub, 1, "the unpublished one isn't named")
	priv, err := albums.AlbumsOf(all[1].ID, false)
	require.NoError(t, err)
	assert.Len(t, priv, 2)
}

func get(h http.Handler, url string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
	return w
}

func TestMapSpots(t *testing.T) {
	_, h, svc := newTestAdmin(t)
	seedFilterPhotos(t, svc)

	var spots []Spot
	w := get(h, "/gallery/map.json")
	require.Equal(t, 200, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &spots))
	assert.Len(t, spots, 3, "only photos that know where they are")
	assert.Equal(t, "d2023-end", spots[0].Title)
	assert.InDelta(t, 35.6, spots[0].Lat, 1e-9)
	assert.InDelta(t, 139.7, spots[0].Lng, 1e-9)
	assert.Equal(t, "Dec 31, 2023", spots[0].Date)
	assert.True(t, strings.HasPrefix(spots[0].Thumb, "/i/photos/"), spots[0].Thumb)

	w = get(h, "/gallery/map.json?year=2023&source=instagram")
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &spots))
	require.Len(t, spots, 1)
	assert.Equal(t, "c2023-mid", spots[0].Title)

	// no photos is an empty list, which the map can draw, not null
	w = get(h, "/gallery/map.json?year=1999")
	assert.JSONEq(t, `[]`, w.Body.String())
}

func TestChangeCollection(t *testing.T) {
	a, h, svc := newTestAdmin(t)
	seedFilterPhotos(t, svc)
	albums := NewAlbumService(a.db)
	c, err := albums.Create("Paris", "")
	require.NoError(t, err)
	all, err := svc.Select(OrderNewest)
	require.NoError(t, err)

	postForm := func(path string, ids ...string) (int, map[string]any) {
		form := url.Values{"ids": ids}
		req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		var out map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
		return w.Code, out
	}

	code, out := postForm("/gallery/collections/"+c.ID+"/add", all[0].ID, all[1].ID, "no-such-photo")
	assert.Equal(t, 200, code)
	assert.Equal(t, true, out["success"])
	in, err := albums.Photos(c.ID, false)
	require.NoError(t, err)
	assert.Equal(t, []string{all[0].ID, all[1].ID}, []string{in[0].ID, in[1].ID}, "in the order given; strangers skipped")

	code, _ = postForm("/gallery/collections/"+c.ID+"/remove", all[0].ID)
	assert.Equal(t, 200, code)
	in, _ = albums.Photos(c.ID, false)
	assert.Len(t, in, 1)

	code, out = postForm("/gallery/collections/" + c.ID + "/add")
	assert.Equal(t, 400, code)
	assert.Equal(t, false, out["success"])
	code, _ = postForm("/gallery/collections/nope/add", all[0].ID)
	assert.Equal(t, 404, code)
}

func postRaw(h http.Handler, path, ctype string, body *strings.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, body)
	req.Header.Set("Content-Type", ctype)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestCreateCollection(t *testing.T) {
	a, h, svc := newTestAdmin(t)
	seedFilterPhotos(t, svc)
	albums := NewAlbumService(a.db)
	all, err := svc.Select(OrderNewest)
	require.NoError(t, err)

	form := url.Values{"title": {"  Summer  "}, "description": {" warm "}, "ids": {all[0].ID, all[1].ID}}
	w := postRaw(h, "/gallery/collections/", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	require.Equal(t, 200, w.Code, w.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	id, _ := out["id"].(string)
	assert.Equal(t, "/admin/gallery/collections/"+id, out["url"])

	c, err := albums.Get(id)
	require.NoError(t, err)
	assert.Equal(t, "Summer", c.Title)
	assert.Equal(t, "warm", c.Description)
	in, err := albums.Photos(id, false)
	require.NoError(t, err)
	assert.Len(t, in, 2, "started with the photos that were selected")

	// no photos is fine
	w = postRaw(h, "/gallery/collections/", "application/x-www-form-urlencoded", strings.NewReader("title=Bare"))
	assert.Equal(t, 200, w.Code)

	// a title is needed, and says so as json
	w = postRaw(h, "/gallery/collections/", "application/x-www-form-urlencoded", strings.NewReader("title=+++"))
	assert.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "needs a title")
}

// a browser's FormData is multipart, and has to work as much as a plain form
func TestChangeCollectionMultipart(t *testing.T) {
	a, h, svc := newTestAdmin(t)
	seedFilterPhotos(t, svc)
	albums := NewAlbumService(a.db)
	c, err := albums.Create("Multi", "")
	require.NoError(t, err)
	all, err := svc.Select(OrderNewest)
	require.NoError(t, err)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("ids", all[0].ID))
	require.NoError(t, mw.WriteField("ids", all[1].ID))
	require.NoError(t, mw.Close())
	w := postRaw(h, "/gallery/collections/"+c.ID+"/add", mw.FormDataContentType(), strings.NewReader(buf.String()))
	require.Equal(t, 200, w.Code, w.Body.String())
	in, err := albums.Photos(c.ID, false)
	require.NoError(t, err)
	assert.Len(t, in, 2)
}

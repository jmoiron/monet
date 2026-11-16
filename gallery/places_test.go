package gallery

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jmoiron/monet/pkg/classify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// geoData is the files of pkg/geo for two cities, in a directory.
func geoData(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	row := func(id, name, lat, lng, cc, admin1, pop string) string {
		return id + "\t" + name + "\t" + name + "\t\t" + lat + "\t" + lng + "\tP\tPPL\t" + cc + "\t\t" + admin1 + "\t\t\t\t" + pop + "\t\t\tTZ\t2020-01-01\n"
	}
	f, err := os.Create(filepath.Join(dir, "cities1000.zip"))
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("cities1000.txt")
	w.Write([]byte(row("1", "Tokyo", "35.6895", "139.69171", "JP", "40", "8336599") +
		row("2", "Hoboken", "40.74", "-74.03", "US", "NJ", "50000")))
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
	require.NoError(t, os.WriteFile(filepath.Join(dir, "countryInfo.txt"), []byte(
		"JP\tJPN\t392\tJA\tJapan\tTokyo\t0\t0\tAS\t.jp\tJPY\tYen\t81\t\t\tja\t1861060\n"+
			"US\tUSA\t840\tUS\tUnited States\tWashington\t0\t0\tNA\t.us\tUSD\tDollar\t1\t\t\ten-US\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "admin1CodesASCII.txt"), []byte("JP.40\tTokyo\tTokyo\t1850144\nUS.NJ\tNew Jersey\tNew Jersey\t2\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "local-names.tsv"), []byte("1\tja\t東京\n1850144\tja\t東京都\n1861060\tja\t日本\n"), 0o644))
	return dir
}

func addPhotoAt(t *testing.T, svc *PhotoService, n int, lat, lng float64) *Photo {
	t.Helper()
	p, err := svc.Add(NewPhoto{Source: SourceUpload, Data: testJPEG(t, 20+n, 20), Latitude: &lat, Longitude: &lng, Published: true})
	require.NoError(t, err)
	return p
}

func TestPlacesAreSearched(t *testing.T) {
	svc := newTestService(t)
	places := NewPlaces(svc.db, geoData(t))
	svc.places = places

	// before there is data, a photo is added without a place
	early := addPhotoAt(t, svc, 0, 35.69, 139.69)
	pl, err := places.Of(early.ID)
	require.NoError(t, err)
	assert.Nil(t, pl)
	assert.False(t, places.Status().Ready)

	// when the server starts it loads the data and places the photos there
	places.Ensure(context.Background())
	st := places.Status()
	assert.True(t, st.Ready)
	assert.Equal(t, PlaceStatus{Ready: true, Cities: 2, WithGPS: 1, Placed: 1}, st)
	pl, err = places.Of(early.ID)
	require.NoError(t, err)
	require.NotNil(t, pl)
	assert.Equal(t, "Tokyo", pl.City)
	assert.Equal(t, "Japan", pl.Country)
	assert.Equal(t, "Asia", pl.Continent)
	assert.Equal(t, "東京", pl.Local)
	assert.Equal(t, "東京都", pl.RegionLocal)
	assert.Equal(t, "日本", pl.CountryLocal)

	// a photo added now is placed as it is added; one without coordinates is not
	hoboken := addPhotoAt(t, svc, 1, 40.74, -74.03)
	plain, err := svc.Add(NewPhoto{Source: SourceUpload, Data: testJPEG(t, 30, 20), Published: true})
	require.NoError(t, err)
	pl, _ = places.Of(hoboken.ID)
	require.NotNil(t, pl)
	assert.Equal(t, "Hoboken|New Jersey|United States", pl.City+"|"+pl.Region+"|"+pl.Country)
	pl, _ = places.Of(plain.ID)
	assert.Nil(t, pl)

	s := NewSearcher(svc.db, SearchConfig{})
	ids := func(q string) []string {
		res, err := s.Search(context.Background(), q, SearchOptions{})
		require.NoError(t, err)
		var out []string
		for _, m := range res.Matches {
			out = append(out, m.ID)
		}
		return out
	}
	// by where, with no model having seen the photo
	assert.Equal(t, []string{early.ID}, ids("tokyo"))
	assert.Equal(t, []string{early.ID}, ids("japan"))
	assert.Equal(t, []string{early.ID}, ids("asia"))
	assert.Equal(t, []string{hoboken.ID}, ids("jersey"))
	// by the start of a name, and by the name in the local language
	assert.Equal(t, []string{early.ID}, ids("tok"))
	assert.Equal(t, []string{early.ID}, ids("東京"))
	assert.Equal(t, []string{early.ID}, ids("日本"))
	assert.Empty(t, ids("kyoto"))

	// with what a model saw, in one search
	_, err = NewClassService(svc.db).Import([]classify.Item{
		item(early, "gemma", "A sunset over rooftops", []string{"sunset"}, nil),
		item(hoboken, "gemma", "A sunset over a river", []string{"sunset"}, nil),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{early.ID}, ids("tokyo sunset"))
	assert.ElementsMatch(t, []string{early.ID, hoboken.ID}, ids("sunset"))
	res, err := s.Search(context.Background(), "tokyo", SearchOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, res.Matches[0].Why.Words)
	assert.Contains(t, res.Matches[0].Why.Words[0].Text(), "[Tokyo]")

	// the places are made again from the data: a rescan keeps what was
	n, err := places.Scan(true)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, []string{early.ID}, ids("tokyo sunset"))

	// deleting a photo takes its place
	require.NoError(t, svc.Delete(early.ID))
	assert.Empty(t, ids("tokyo"))
	var count int
	require.NoError(t, svc.db.Get(&count, `SELECT count(*) FROM photo_place`))
	assert.Equal(t, 1, count)
}

func TestPlacesWithoutADirectory(t *testing.T) {
	svc := newTestService(t)
	svc.places = NewPlaces(svc.db, "")
	p := addPhotoAt(t, svc, 0, 35.69, 139.69)
	pl, err := svc.places.Of(p.ID)
	require.NoError(t, err)
	assert.Nil(t, pl)
	assert.False(t, svc.places.Enabled())
	_, err = svc.places.Scan(true)
	assert.Error(t, err)
}

package geo

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testFiles writes a few real cities as the files that Load reads, named as
// Fetch gets them, and returns the directory.
func testFiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	n := 0
	row := func(name, lat, lng, cc, admin1, pop string) string {
		n++
		return strconv.Itoa(n) + "\t" + name + "\t" + name + "\t\t" + lat + "\t" + lng + "\tP\tPPL\t" + cc + "\t\t" + admin1 + "\t\t\t\t" + pop + "\t\t\tTZ\t2020-01-01\n"
	}
	f, err := os.Create(filepath.Join(dir, citiesFile))
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("cities1000.txt")
	w.Write([]byte(row("Tokyo", "35.6895", "139.69171", "JP", "40", "8336599") +
		row("Shibuya", "35.66", "139.70", "JP", "40", "220000") +
		row("Guangzhou", "23.11667", "113.25", "CN", "30", "2000000") +
		row("Auckland", "-36.85", "174.76", "NZ", "E7", "400000") +
		row("Suva", "-18.14", "178.44", "FJ", "01", "90000") +
		row("Tiny", "35.70", "139.71", "JP", "40", "1200")))
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
	require.NoError(t, os.WriteFile(filepath.Join(dir, countryFile), []byte("# comment\nJP\tJPN\t392\tJA\tJapan\tTokyo\t0\t0\tAS\t.jp\tJPY\tYen\t81\t\t\tja\t1861060\n"+
		"CN\tCHN\t156\tCH\tChina\tBeijing\t0\t0\tAS\nNZ\tNZL\t554\tNZ\tNew Zealand\tWellington\t0\t0\tOC\n"+
		"FJ\tFJI\t242\tFJ\tFiji\tSuva\t0\t0\tOC\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, adminFile), []byte("JP.40\tTokyo\tTokyo\t1850144\nCN.30\tGuangdong\tGuangdong\t1809935\n"), 0o644))
	return dir
}

func testIndex(t *testing.T) *Index {
	t.Helper()
	dir := testFiles(t)
	ix, err := Load(filepath.Join(dir, citiesFile), filepath.Join(dir, countryFile), filepath.Join(dir, adminFile))
	require.NoError(t, err)
	return ix
}

func TestNearest(t *testing.T) {
	ix := testIndex(t)
	assert.Equal(t, 6, ix.Len())

	p := ix.Nearest(35.69, 139.69) // Shinjuku
	assert.Equal(t, "Tokyo", p.City)
	assert.Equal(t, "Tokyo, Japan", p.String(), "a region that is the city's name isn't said twice")
	assert.Equal(t, "Asia", p.Continent)
	assert.Less(t, p.Km, 15.0)

	p = ix.Nearest(23.0, 113.0)
	assert.Equal(t, "Guangzhou, Guangdong, China", p.String())

	// across the date line: Fiji is nearer to a point at -179.9 than Auckland
	p = ix.Nearest(-18.0, -179.9)
	assert.Equal(t, "Suva", p.City)
	assert.Equal(t, "Oceania", p.Continent)
}

func TestLocalNames(t *testing.T) {
	ix := testIndex(t)
	dir := t.TempDir()
	// Tokyo is the first city of the test index, and has the id 1
	alt := filepath.Join(dir, "alt.txt")
	row := func(id, lang, name, preferred, short, coll, hist string) string {
		return "0\t" + id + "\t" + lang + "\t" + name + "\t" + preferred + "\t" + short + "\t" + coll + "\t" + hist + "\n"
	}
	require.NoError(t, os.WriteFile(alt, []byte(
		row("1", "en", "Tokyo", "1", "", "", "")+
			row("1", "ko", "도쿄", "1", "", "", "")+ // not a language of Japan
			row("1", "ja_rm", "Tōkyō", "", "", "", "")+ // romaji, not japanese
			row("1", "ja", "東京", "", "1", "", "")+ // a short name is still a name
			row("1850144", "ja", "東京都", "1", "", "", "")+ // the region, in the language of the country
			row("1850144", "ja", "Tokyo", "", "", "", "")+ // the same as the English one is not a local name
			row("1861060", "ja", "日本", "1", "", "", "")+ // and the country
			row("1", "ja", "東京都", "1", "", "", "1")+ // historic: not wanted
			row("1", "zh", "东京", "", "", "", "")), 0o644))
	out := filepath.Join(dir, "local.tsv")
	require.NoError(t, ix.LocalNames(alt, out))
	require.NoError(t, ix.LoadLocalNames(out))
	p := ix.Nearest(35.69, 139.69)
	assert.Equal(t, "東京", p.Local)
	assert.Equal(t, "ja", p.LocalLang)
	assert.Equal(t, "東京都", p.RegionLocal)
	assert.Equal(t, "日本", p.CountryLocal)
}

func TestLocalLanguages(t *testing.T) {
	c := func(l ...string) country { return country{languages: l} }
	assert.Equal(t, []string{"ja"}, localLanguages("JP", c("ja")))
	assert.Equal(t, []string{"zh-hk", "yue", "zh"}, localLanguages("HK", c("zh-HK", "yue", "zh", "en")))
	assert.Nil(t, localLanguages("US", c("en-US", "es-US")), "english first: no other language is the local one")
	assert.Equal(t, []string{"hi", "bn"}, localLanguages("IN", c("en-IN", "hi", "bn")))
}

func TestAround(t *testing.T) {
	ix := testIndex(t)
	// the town nearest to this point is Tiny, and the nearest of 5000 (and of
	// 15000, which is the same) is Tokyo, a little nearer than Shibuya:

	got := ix.Around(35.70, 139.71, 0, 5000, 15000)
	var names []string
	for _, p := range got {
		names = append(names, p.City)
	}
	assert.Equal(t, []string{"Tiny", "Tokyo"}, names)
	// nothing is as large as that
	assert.Empty(t, ix.Around(35.70, 139.71, 1_000_000_000))
}

func TestFetch(t *testing.T) {
	files := testFiles(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		if name == alternates {
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			f, _ := zw.Create("alternateNamesV2.txt")
			f.Write([]byte("0\t1\tja\t東京\t\t1\t\t\n")) // Tokyo is the id 1
			zw.Close()
			w.Write(buf.Bytes())
			return
		}
		http.ServeFile(w, r, filepath.Join(files, name))
	}))
	defer srv.Close()
	old := DumpURL
	DumpURL = srv.URL + "/"
	defer func() { DumpURL = old }()

	into := filepath.Join(t.TempDir(), "geo")
	_, err := Open(into)
	assert.ErrorIs(t, err, ErrNoData)

	require.NoError(t, Fetch(context.Background(), into))
	assert.True(t, Present(into))
	_, err = os.Stat(filepath.Join(into, alternates))
	assert.True(t, os.IsNotExist(err), "the large file is not kept")

	got, err := Open(into)
	require.NoError(t, err)
	assert.Equal(t, "東京", got.Nearest(35.69, 139.69).Local)

	// an alternateNamesV2.zip that is already there is used, and not fetched
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("alternateNamesV2.txt")
	f.Write([]byte("0\t1\tja\tトウキョウ\t\t1\t\t\n"))
	zw.Close()
	require.NoError(t, os.WriteFile(filepath.Join(into, alternates), buf.Bytes(), 0o644))
	require.NoError(t, Fetch(context.Background(), into))
	got, err = Open(into)
	require.NoError(t, err)
	assert.Equal(t, "トウキョウ", got.Nearest(35.69, 139.69).Local, "the file that was there made the names")
	_, err = os.Stat(filepath.Join(into, alternates))
	assert.NoError(t, err, "and it is left")
}

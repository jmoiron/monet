package bookmarks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
)

func newTestService(t *testing.T) *BookmarkService {
	t.Helper()
	db, err := sqlx.Connect("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := NewApp(db).Migrate(); err != nil {
		t.Fatal(err)
	}
	return NewBookmarkService(db)
}

func TestCardFieldsRoundTrip(t *testing.T) {
	assert := assert.New(t)
	serv := newTestService(t)

	fetched := time.Now().Truncate(time.Second)
	b := &Bookmark{URL: "https://www.example.com/a", Title: "mine", OGTitle: "Site's Title",
		OGDescription: "site desc", SiteName: "Example", OGImagePath: "/x/o.jpg", OGFetchedAt: fetched}
	assert.NoError(serv.Save(b)) // insert
	got, err := serv.GetByID(b.ID)
	assert.NoError(err)
	assert.Equal("Site's Title", got.OGTitle)
	assert.Equal("site desc", got.OGDescription)
	assert.Equal("Example", got.SiteName)
	assert.True(got.OGFetchedAt.Equal(fetched), "fetched %v != %v", got.OGFetchedAt, fetched)

	got.OGDescription = "changed"
	assert.NoError(serv.Save(got)) // update
	got2, _ := serv.GetByID(b.ID)
	assert.Equal("changed", got2.OGDescription)

	// a bookmark that never loaded a card still reads back fine
	plain := &Bookmark{URL: "https://plain.example", Title: "plain"}
	assert.NoError(serv.Save(plain))
	p2, err := serv.GetByID(plain.ID)
	assert.NoError(err)
	assert.False(p2.HasCard())
	assert.LessOrEqual(p2.OGFetchedAt.Unix(), int64(0))
}

func TestCardHelpers(t *testing.T) {
	b := &Bookmark{URL: "https://WWW.Example.com:8080/x", Title: "mine"}
	assert.Equal(t, "example.com", b.Domain())
	assert.Equal(t, "example.com", b.CardSource())
	assert.Equal(t, "mine", b.CardTitle())
	b.SiteName, b.OGTitle = "Example", "Theirs"
	assert.Equal(t, "Example", b.CardSource())
	assert.Equal(t, "Theirs", b.CardTitle())
	assert.True(t, b.HasCard())
}

// cardSite serves a page whose metadata can be changed between loads.
type cardSite struct {
	srv  *httptest.Server
	page atomic.Value // string
}

func newCardSite(t *testing.T, page string) *cardSite {
	cs := &cardSite{}
	cs.page.Store(page)
	cs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/img") {
			w.Write([]byte("\x89PNG\r\n\x1a\n" + r.URL.Path))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(cs.page.Load().(string)))
	}))
	t.Cleanup(cs.srv.Close)
	return cs
}

const page1 = `<title>Page</title><meta property="og:title" content="Title 1"><meta property="og:description" content="Desc 1">
<meta property="og:site_name" content="Site 1"><meta property="og:image" content="/img1.png">`
const page2 = `<title>Page</title><meta property="og:title" content="Title 2"><meta property="og:description" content="Desc 2">
<meta property="og:image" content="/img2.png">`

func TestLoadCardCreateThenReload(t *testing.T) {
	assert := assert.New(t)
	site := newCardSite(t, page1)
	dir := t.TempDir()
	ctx := context.Background()

	// the add flow: nothing set yet, fills everything
	b := &Bookmark{URL: site.srv.URL}
	updated, err := LoadCard(ctx, newBrowserClient(), b, dir, false)
	assert.NoError(err)
	assert.ElementsMatch([]string{"og_title", "og_description", "site_name", "title", "og_image"}, updated)
	assert.Equal("Title 1", b.Title, "empty bookmark title is taken from the card")
	assert.Equal("Desc 1", b.OGDescription)
	assert.Empty(b.Description, "the owner's description is never filled from the site")
	assert.False(b.OGFetchedAt.IsZero())
	img1 := b.OGImagePath
	assert.FileExists(img1)

	// the owner renames the bookmark and writes a description
	b.Title, b.Description = "My Name", "my words"

	// the site changes; a reload replaces the card but not the owner's fields
	site.page.Store(page2)
	updated, err = LoadCard(ctx, newBrowserClient(), b, dir, true)
	assert.NoError(err)
	assert.ElementsMatch([]string{"og_title", "og_description", "og_image"}, updated)
	assert.Equal("Title 2", b.OGTitle)
	assert.Equal("Desc 2", b.OGDescription)
	assert.Equal("Site 1", b.SiteName, "a field the site no longer provides is left alone")
	assert.Equal("My Name", b.Title)
	assert.Equal("my words", b.Description)
	assert.NotEqual(img1, b.OGImagePath)
	assert.FileExists(b.OGImagePath)
	_, err = os.Stat(img1)
	assert.True(os.IsNotExist(err), "the replaced image file should be removed")
}

func TestLoadCardFillOnlyDoesNotOverwrite(t *testing.T) {
	assert := assert.New(t)
	site := newCardSite(t, page1)
	b := &Bookmark{URL: site.srv.URL, Title: "keep", OGTitle: "old", OGDescription: "old desc"}
	_, err := LoadCard(context.Background(), newBrowserClient(), b, t.TempDir(), false)
	assert.NoError(err)
	assert.Equal("keep", b.Title)
	assert.Equal("old", b.OGTitle)
	assert.Equal("old desc", b.OGDescription)
	assert.Equal("Site 1", b.SiteName, "empty fields are still filled")
}

func TestLoadCardNoMetadataChangesNothing(t *testing.T) {
	assert := assert.New(t)
	site := newCardSite(t, `<html><body>hello</body></html>`)
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep-og.png")
	os.WriteFile(keep, []byte("x"), 0644)

	b := &Bookmark{URL: site.srv.URL, OGTitle: "good card", OGImagePath: keep}
	_, err := LoadCard(context.Background(), newBrowserClient(), b, dir, true)
	assert.Error(err)
	assert.Equal("good card", b.OGTitle)
	assert.Equal(keep, b.OGImagePath)
	assert.FileExists(keep)
	assert.True(b.OGFetchedAt.IsZero(), "a failed load must not restamp the card")
}

func searchIDs(t *testing.T, serv *BookmarkService, q string) []string {
	t.Helper()
	found, err := serv.Search(q, 10, 0)
	if err != nil {
		t.Fatalf("search %q: %v", q, err)
	}
	var ids []string
	for _, b := range found {
		ids = append(ids, b.ID)
	}
	return ids
}

func TestSearchCoversCard(t *testing.T) {
	assert := assert.New(t)
	serv := newTestService(t)

	b := &Bookmark{URL: "https://zoo.example/q", Title: "plain name", Published: 1,
		OGTitle: "Quokka Quarterly", OGDescription: "marsupials of western australia", SiteName: "Zoology Weekly"}
	assert.NoError(serv.Save(b))

	// the card's title, description and site name are all searchable
	for _, q := range []string{"marsupials", "quokka", "zoology", "plain"} {
		assert.Equal([]string{b.ID}, searchIDs(t, serv, q), q)
	}
	n, err := serv.SearchCount("marsupials")
	assert.NoError(err)
	assert.Equal(1, n)

	// a reload that changes the card is reflected
	b.OGDescription = "wombats of tasmania"
	assert.NoError(serv.Save(b))
	assert.Empty(searchIDs(t, serv, "marsupials"))
	assert.Equal([]string{b.ID}, searchIDs(t, serv, "wombats"))

	// unpublished bookmarks stay out of public search
	b.Published = 0
	assert.NoError(serv.Save(b))
	assert.Empty(searchIDs(t, serv, "wombats"))
	b.Published = 1
	assert.NoError(serv.Save(b))

	assert.NoError(serv.DeleteByID(b.ID))
	assert.Empty(searchIDs(t, serv, "wombats"))
}

func TestSearchMigrationDowngradeAndRepeat(t *testing.T) {
	assert := assert.New(t)
	db, err := sqlx.Connect("sqlite3", ":memory:")
	assert.NoError(err)
	assert.NoError(NewApp(db).Migrate())
	serv := NewBookmarkService(db)

	b := &Bookmark{URL: "https://zoo.example/q", Title: "plain name", Published: 1, OGDescription: "marsupials"}
	assert.NoError(serv.Save(b))
	assert.Equal([]string{b.ID}, searchIDs(t, serv, "marsupials"))

	// downgrading the fts migration goes back to title/url/description only...
	m, err := monarch.NewManager(db)
	assert.NoError(err)
	assert.NoError(m.Downgrade("bookmark")) // image_choice
	assert.NoError(m.Downgrade("bookmark")) // the card in the index
	_, err = db.Exec(`SELECT og_description FROM bookmark_fts LIMIT 1`)
	assert.Error(err, "old index has no og_description column")
	assert.Equal([]string{b.ID}, searchIDs(t, serv, "plain"), "existing fields still searchable")

	// ...and upgrading again rebuilds the index from existing rows
	assert.NoError(NewApp(db).Migrate())
	assert.Equal([]string{b.ID}, searchIDs(t, serv, "marsupials"))
}

// the index is maintained entirely by triggers; deleting (or updating) rows
// directly, with no rebuild afterwards, must leave search working and correct.
func TestSearchIndexMaintainedByTriggersAlone(t *testing.T) {
	assert := assert.New(t)
	serv := newTestService(t)

	var keep, drop Bookmark
	keep = Bookmark{URL: "https://a.example", Title: "alpha keeper", Published: 1}
	drop = Bookmark{URL: "https://b.example", Title: "beta dropper", Published: 1}
	assert.NoError(serv.Save(&keep))
	assert.NoError(serv.Save(&drop))

	_, err := serv.db.Exec(`DELETE FROM bookmark WHERE id=?`, drop.ID) // as the admin delete does
	assert.NoError(err)
	assert.Empty(searchIDs(t, serv, "dropper"))
	assert.Equal([]string{keep.ID}, searchIDs(t, serv, "keeper"), "results map back to the right bookmark")

	// rows added after a delete reuse index positions safely
	again := Bookmark{URL: "https://c.example", Title: "gamma again", Published: 1}
	assert.NoError(serv.Save(&again))
	assert.Equal([]string{again.ID}, searchIDs(t, serv, "gamma"))
	assert.Equal([]string{keep.ID}, searchIDs(t, serv, "alpha"))

	var check int
	_, err = serv.db.Exec(`INSERT INTO bookmark_fts(bookmark_fts, rank) VALUES ('integrity-check', 1)`)
	assert.NoError(err)
	assert.NoError(serv.db.Get(&check, `SELECT count(*) FROM bookmark_fts`))
	assert.Equal(2, check)
}

func TestImageSelection(t *testing.T) {
	cases := []struct {
		name                 string
		b                    Bookmark
		kind, display, thumb string
	}{
		{"nothing", Bookmark{}, "", "", ""},
		{"og only", Bookmark{OGImagePath: "o.jpg"}, ImageOG, "o.jpg", "o.jpg"},
		{"screenshot only", Bookmark{ScreenshotPath: "s.jpg", IconPath: "s-i.jpg"}, ImageScreenshot, "s.jpg", "s-i.jpg"},
		{"screenshot without icon", Bookmark{ScreenshotPath: "s.jpg"}, ImageScreenshot, "s.jpg", "s.jpg"},
		{"both, auto prefers the site's image", Bookmark{OGImagePath: "o.jpg", ScreenshotPath: "s.jpg", IconPath: "s-i.jpg"}, ImageOG, "o.jpg", "o.jpg"},
		{"both, chose screenshot", Bookmark{ImageChoice: ImageScreenshot, OGImagePath: "o.jpg", ScreenshotPath: "s.jpg", IconPath: "s-i.jpg"}, ImageScreenshot, "s.jpg", "s-i.jpg"},
		{"both, chose og", Bookmark{ImageChoice: ImageOG, OGImagePath: "o.jpg", ScreenshotPath: "s.jpg", IconPath: "s-i.jpg"}, ImageOG, "o.jpg", "o.jpg"},
		{"chose screenshot but there isn't one", Bookmark{ImageChoice: ImageScreenshot, OGImagePath: "o.jpg"}, ImageOG, "o.jpg", "o.jpg"},
		{"chose og but there isn't one", Bookmark{ImageChoice: ImageOG, ScreenshotPath: "s.jpg"}, ImageScreenshot, "s.jpg", "s.jpg"},
		{"unknown choice is auto", Bookmark{ImageChoice: "bogus", OGImagePath: "o.jpg", ScreenshotPath: "s.jpg"}, ImageOG, "o.jpg", "o.jpg"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.kind, c.b.ImageKind())
			assert.Equal(t, c.display, c.b.DisplayImagePath())
			assert.Equal(t, c.thumb, c.b.ThumbPath())
		})
	}
}

func TestApplyForm(t *testing.T) {
	assert := assert.New(t)
	b := &Bookmark{OGTitle: "card", OGDescription: "card desc", SiteName: "Site", Published: 0}

	b.applyForm(url.Values{"url": {"https://x.example"}, "title": {"mine"}, "description": {"words"},
		"image": {"screenshot"}, "published": {"1"}})
	assert.Equal("https://x.example", b.URL)
	assert.Equal("mine", b.Title)
	assert.Equal("words", b.Description)
	assert.Equal(ImageScreenshot, b.ImageChoice)
	assert.Equal(1, b.Published)
	assert.False(b.PublishedAt.IsZero())
	assert.Equal("card", b.OGTitle, "the card is not editable from the form")
	assert.Equal("card desc", b.OGDescription)

	// back to auto, and junk is ignored
	b.applyForm(url.Values{"image": {""}, "published": {"1"}})
	assert.Equal(ImageAuto, b.ImageChoice)
	b.ImageChoice = ImageOG
	b.applyForm(url.Values{"image": {"<script>"}, "published": {"1"}})
	assert.Equal(ImageOG, b.ImageChoice, "an unknown value leaves the choice alone")

	// unpublishing clears the publish time
	b.applyForm(url.Values{"image": {"og"}, "published": {"0"}})
	assert.Equal(0, b.Published)
	assert.True(b.PublishedAt.IsZero())
}

func TestImageChoiceRoundTripAndTitleFallback(t *testing.T) {
	assert := assert.New(t)
	serv := newTestService(t)
	b := &Bookmark{URL: "https://www.example.com/x", ImageChoice: ImageScreenshot, ScreenshotPath: "s.jpg", OGImagePath: "o.jpg"}
	assert.NoError(serv.Save(b))
	got, err := serv.GetByID(b.ID)
	assert.NoError(err)
	assert.Equal(ImageScreenshot, got.ImageChoice)
	assert.Equal("s.jpg", got.DisplayImagePath())

	got.ImageChoice = ImageOG
	assert.NoError(serv.Save(got))
	got2, _ := serv.GetByID(b.ID)
	assert.Equal(ImageOG, got2.ImageChoice)

	// a bookmark with no titles at all still has something to show on its card
	assert.Equal("example.com", (&Bookmark{URL: "https://www.example.com/x"}).CardTitle())
}

func TestClearImage(t *testing.T) {
	assert := assert.New(t)
	full := func() *Bookmark {
		return &Bookmark{OGImagePath: "d/o-og.jpg", ScreenshotPath: "d/s.jpg", IconPath: "d/s-256x160.jpg", ImageChoice: ImageOG}
	}

	b := full()
	files, err := b.ClearImage(ImageOG)
	assert.NoError(err)
	assert.Equal([]string{"d/o-og.jpg"}, files)
	assert.Empty(b.OGImagePath)
	assert.Equal("d/s.jpg", b.ScreenshotPath, "the other image is untouched")
	assert.Equal(ImageAuto, b.ImageChoice, "deleting the chosen image resets the choice")
	assert.Equal(ImageScreenshot, b.ImageKind(), "and the other image now shows")

	b = full()
	files, err = b.ClearImage(ImageScreenshot)
	assert.NoError(err)
	assert.Equal([]string{"d/s.jpg", "d/s-256x160.jpg", "d/s.json"}, files, "a screenshot is the image, its icon and its json")
	assert.Empty(b.ScreenshotPath)
	assert.Empty(b.IconPath)
	assert.Equal("d/o-og.jpg", b.OGImagePath)
	assert.Equal(ImageOG, b.ImageChoice, "deleting the other image keeps the choice")

	b = &Bookmark{}
	files, err = b.ClearImage(ImageOG)
	assert.NoError(err)
	assert.Empty(files, "nothing to delete when there was no image")

	_, err = full().ClearImage("bogus")
	assert.Error(err)
}

func TestRemoveFiles(t *testing.T) {
	assert := assert.New(t)
	root := t.TempDir()
	dir := filepath.Join(root, "ss")
	assert.NoError(os.MkdirAll(dir, 0755))
	write := func(p string) string {
		assert.NoError(os.WriteFile(p, []byte("x"), 0644))
		return p
	}
	a, b := write(filepath.Join(dir, "a.jpg")), write(filepath.Join(dir, "b.jpg"))
	outside := write(filepath.Join(root, "precious.txt"))
	sibling := write(filepath.Join(root, "ss-other.txt")) // shares the dir's name as a prefix

	// inside files go, files that are already missing are fine
	assert.NoError(RemoveFiles(dir, []string{a, filepath.Join(dir, "gone.jpg"), b}))
	assert.NoFileExists(a)
	assert.NoFileExists(b)

	// nothing outside the directory is ever deleted, however it's spelled
	for _, p := range []string{
		outside,
		sibling,
		filepath.Join(dir, "..", "precious.txt"),
		dir,
		root,
		"/etc/hostname",
	} {
		assert.Error(RemoveFiles(dir, []string{p}), p)
	}
	assert.FileExists(outside)
	assert.FileExists(sibling)

	// a bad path doesn't stop the good ones being removed, and is still reported
	c := write(filepath.Join(dir, "c.jpg"))
	assert.Error(RemoveFiles(dir, []string{outside, c}))
	assert.NoFileExists(c)
	assert.FileExists(outside)

	// relative paths, as the screenshot service stores them, work too
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	assert.NoError(os.Chdir(root))
	d := write(filepath.Join(dir, "d.jpg"))
	_ = d
	assert.NoError(RemoveFiles("ss", []string{"ss/d.jpg"}))
	assert.NoFileExists(filepath.Join(dir, "d.jpg"))
}

func TestLoadedShort(t *testing.T) {
	assert := assert.New(t)
	assert.Empty((&Bookmark{}).LoadedShort(), "never loaded")

	now := time.Now()
	assert.Equal(now.Format("Jan 2"), (&Bookmark{OGFetchedAt: now}).LoadedShort())
	old := time.Date(now.Year()-2, time.March, 4, 12, 0, 0, 0, time.Local)
	assert.Equal(old.Format("Jan 2, 2006"), (&Bookmark{OGFetchedAt: old}).LoadedShort(), "other years show the year")
}

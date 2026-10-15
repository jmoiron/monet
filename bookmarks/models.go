package bookmarks

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/sqlx"
)

var bookmarkMigrations = monarch.Set{
	Name: "bookmark",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS bookmark (
				id text PRIMARY KEY,
				url text NOT NULL,
				title text,
				description text,
				description_rendered text,
				screenshot_path text,
				published integer DEFAULT 0,
				created_at datetime DEFAULT (strftime('%s', 'now')),
				updated_at datetime DEFAULT (strftime('%s', 'now')),
				published_at datetime DEFAULT 0
			);`,
			Down: `DROP TABLE bookmark;`,
		},
		{
			Up:   `CREATE INDEX IF NOT EXISTS idx_bookmark_published ON bookmark(published);`,
			Down: `DROP INDEX idx_bookmark_published;`,
		},
		{
			Up:   `CREATE INDEX IF NOT EXISTS idx_bookmark_created_at ON bookmark(created_at);`,
			Down: `DROP INDEX idx_bookmark_created_at;`,
		},
		{
			Up: `CREATE VIRTUAL TABLE bookmark_fts USING fts5(
				id UNINDEXED, title, url, description, published,
				content='bookmark',
				tokenize="trigram"
			)`,
			Down: `DROP TABLE bookmark_fts;`,
		},
		{
			Up:   `INSERT INTO bookmark_fts SELECT id, title, url, description, published FROM bookmark;`,
			Down: `DELETE FROM bookmark_fts;`,
		},
		{
			Up: `CREATE TRIGGER bookmark_i AFTER INSERT ON bookmark BEGIN
				INSERT INTO bookmark_fts (id, title, url, description, published) VALUES
					(new.id, new.title, new.url, new.description, new.published);
			END;`,
			Down: `DROP TRIGGER bookmark_i;`,
		},
		{
			Up: `CREATE TRIGGER bookmark_d AFTER DELETE ON bookmark BEGIN
				INSERT INTO bookmark_fts (bookmark_fts, id, title, url, description, published) VALUES
					('delete', old.id, old.title, old.url, old.description, old.published);
			END`,
			Down: `DROP TRIGGER bookmark_d;`,
		},
		{
			Up: `CREATE TRIGGER bookmark_u AFTER UPDATE ON bookmark BEGIN
				INSERT INTO bookmark_fts (bookmark_fts, id, title, url, description, published) VALUES
					('delete', old.id, old.title, old.url, old.description, old.published);
				INSERT INTO bookmark_fts (id, title, url, description, published) VALUES
					(new.id, new.title, new.url, new.description, new.published);
			END`,
			Down: `DROP TRIGGER bookmark_u;`,
		},
		{
			Up:   `ALTER TABLE bookmark ADD COLUMN icon_path text DEFAULT '';`,
			Down: `ALTER TABLE bookmark DROP COLUMN icon_path;`,
		},
		{
			Up:   `ALTER TABLE bookmark ADD COLUMN site_name text DEFAULT '';`,
			Down: `ALTER TABLE bookmark DROP COLUMN site_name;`,
		},
		{
			Up:   `ALTER TABLE bookmark ADD COLUMN og_image_path text DEFAULT '';`,
			Down: `ALTER TABLE bookmark DROP COLUMN og_image_path;`,
		},
		{
			Up:   `ALTER TABLE bookmark ADD COLUMN og_title text DEFAULT '';`,
			Down: `ALTER TABLE bookmark DROP COLUMN og_title;`,
		},
		{
			Up:   `ALTER TABLE bookmark ADD COLUMN og_description text DEFAULT '';`,
			Down: `ALTER TABLE bookmark DROP COLUMN og_description;`,
		},
		{
			Up:   `ALTER TABLE bookmark ADD COLUMN og_fetched_at datetime DEFAULT 0;`,
			Down: `ALTER TABLE bookmark DROP COLUMN og_fetched_at;`,
		},
		{
			// Rebuild the full text index to also cover the card (og title and
			// description, site name). Every statement is idempotent so that a
			// failure partway through can be re-run.
			//
			// bookmark_fts is an external content table, so the triggers must
			// name the bookmark's rowid on insert and delete; without it the index
			// picks its own rowids, 'delete' targets the wrong entry and the index
			// is corrupted (which the earlier triggers papered over by having
			// Save rebuild the index after every write).
			Up: `
				DROP TRIGGER IF EXISTS bookmark_i;
				DROP TRIGGER IF EXISTS bookmark_d;
				DROP TRIGGER IF EXISTS bookmark_u;
				DROP TABLE IF EXISTS bookmark_fts;
				CREATE VIRTUAL TABLE bookmark_fts USING fts5(
					id UNINDEXED, title, url, description, og_title, og_description, site_name, published,
					content='bookmark',
					tokenize="trigram"
				);
				INSERT INTO bookmark_fts(bookmark_fts) VALUES ('rebuild');
				CREATE TRIGGER bookmark_i AFTER INSERT ON bookmark BEGIN
					INSERT INTO bookmark_fts (rowid, id, title, url, description, og_title, og_description, site_name, published) VALUES
						(new.rowid, new.id, new.title, new.url, new.description, new.og_title, new.og_description, new.site_name, new.published);
				END;
				CREATE TRIGGER bookmark_d AFTER DELETE ON bookmark BEGIN
					INSERT INTO bookmark_fts (bookmark_fts, rowid, id, title, url, description, og_title, og_description, site_name, published) VALUES
						('delete', old.rowid, old.id, old.title, old.url, old.description, old.og_title, old.og_description, old.site_name, old.published);
				END;
				CREATE TRIGGER bookmark_u AFTER UPDATE ON bookmark BEGIN
					INSERT INTO bookmark_fts (bookmark_fts, rowid, id, title, url, description, og_title, og_description, site_name, published) VALUES
						('delete', old.rowid, old.id, old.title, old.url, old.description, old.og_title, old.og_description, old.site_name, old.published);
					INSERT INTO bookmark_fts (rowid, id, title, url, description, og_title, og_description, site_name, published) VALUES
						(new.rowid, new.id, new.title, new.url, new.description, new.og_title, new.og_description, new.site_name, new.published);
				END;`,
			Down: `
				DROP TRIGGER IF EXISTS bookmark_i;
				DROP TRIGGER IF EXISTS bookmark_d;
				DROP TRIGGER IF EXISTS bookmark_u;
				DROP TABLE IF EXISTS bookmark_fts;
				CREATE VIRTUAL TABLE bookmark_fts USING fts5(
					id UNINDEXED, title, url, description, published,
					content='bookmark',
					tokenize="trigram"
				);
				INSERT INTO bookmark_fts(bookmark_fts) VALUES ('rebuild');
				CREATE TRIGGER bookmark_i AFTER INSERT ON bookmark BEGIN
					INSERT INTO bookmark_fts (rowid, id, title, url, description, published) VALUES
						(new.rowid, new.id, new.title, new.url, new.description, new.published);
				END;
				CREATE TRIGGER bookmark_d AFTER DELETE ON bookmark BEGIN
					INSERT INTO bookmark_fts (bookmark_fts, rowid, id, title, url, description, published) VALUES
						('delete', old.rowid, old.id, old.title, old.url, old.description, old.published);
				END;
				CREATE TRIGGER bookmark_u AFTER UPDATE ON bookmark BEGIN
					INSERT INTO bookmark_fts (bookmark_fts, rowid, id, title, url, description, published) VALUES
						('delete', old.rowid, old.id, old.title, old.url, old.description, old.published);
					INSERT INTO bookmark_fts (rowid, id, title, url, description, published) VALUES
						(new.rowid, new.id, new.title, new.url, new.description, new.published);
				END;`,
		},
		{
			Up:   `ALTER TABLE bookmark ADD COLUMN image_choice text DEFAULT '';`,
			Down: `ALTER TABLE bookmark DROP COLUMN image_choice;`,
		},
	},
}

type Bookmark struct {
	ID                  string
	URL                 string
	Title               string
	Description         string
	DescriptionRendered string `db:"description_rendered"`
	ScreenshotPath      string `db:"screenshot_path"`
	IconPath            string `db:"icon_path"`
	SiteName            string `db:"site_name"`
	OGImagePath         string `db:"og_image_path"`
	OGTitle             string `db:"og_title"`
	OGDescription       string `db:"og_description"`
	ImageChoice         string `db:"image_choice"` // ImageAuto, ImageOG or ImageScreenshot
	Published           int
	CreatedAt           time.Time `db:"created_at"`
	UpdatedAt           time.Time `db:"updated_at"`
	PublishedAt         time.Time `db:"published_at"`
	OGFetchedAt         time.Time `db:"og_fetched_at"`
}

// The "card" is the snapshot of the site's own metadata (og:title,
// og:description, og:image, og:site_name) taken when the bookmark was last
// loaded; Title and Description are the owner's own, and take precedence.

// applyForm sets the fields the edit form controls. The card (og_*, site name)
// is never taken from the form; it comes from loading the site.
func (b *Bookmark) applyForm(form url.Values) {
	b.URL = form.Get("url")
	b.Title = form.Get("title")
	b.Description = form.Get("description")

	switch c := form.Get("image"); c {
	case ImageAuto, ImageOG, ImageScreenshot:
		b.ImageChoice = c
	}

	if pub, _ := strconv.Atoi(form.Get("published")); b.Published != pub {
		b.Published = pub
		if pub == 0 {
			b.PublishedAt = time.Time{}
		} else {
			b.PublishedAt = time.Now()
		}
	}
}

// HasCard reports whether any card metadata has been loaded.
func (b *Bookmark) HasCard() bool {
	return b.OGTitle != "" || b.OGDescription != "" || b.OGImagePath != "" || b.SiteName != ""
}

// Which of a bookmark's images is shown, here and on the public pages. Auto
// prefers the site's own image (og:image) and falls back to our screenshot.
const (
	ImageAuto       = ""
	ImageOG         = "og"
	ImageScreenshot = "screenshot"
)

// ImageKind resolves which image to show: the owner's choice if that image
// exists, otherwise the og:image if there is one, otherwise the screenshot.
// It is "" when the bookmark has no image at all.
func (b *Bookmark) ImageKind() string {
	hasOG, hasShot := b.OGImagePath != "", b.ScreenshotPath != ""
	switch {
	case b.ImageChoice == ImageScreenshot && hasShot:
		return ImageScreenshot
	case b.ImageChoice == ImageOG && hasOG:
		return ImageOG
	case hasOG:
		return ImageOG
	case hasShot:
		return ImageScreenshot
	}
	return ""
}

// DisplayImagePath is the full-size file for the image ImageKind selects.
func (b *Bookmark) DisplayImagePath() string {
	switch b.ImageKind() {
	case ImageOG:
		return b.OGImagePath
	case ImageScreenshot:
		return b.ScreenshotPath
	}
	return ""
}

// ThumbPath is the file to use for the selected image in lists: the screenshot's
// small icon where there is one, otherwise the full-size image.
func (b *Bookmark) ThumbPath() string {
	if b.ImageKind() == ImageScreenshot && b.IconPath != "" {
		return b.IconPath
	}
	return b.DisplayImagePath()
}

// ClearImage forgets one of b's images ("og" or "screenshot") and returns the
// files that held it, for RemoveFiles. A screenshot is a few files: the image,
// its icon and the page JSON gowitness wrote beside it. If the cleared image was
// the one chosen for display, the choice goes back to auto.
func (b *Bookmark) ClearImage(kind string) ([]string, error) {
	var files []string
	switch kind {
	case ImageOG:
		files = []string{b.OGImagePath}
		b.OGImagePath = ""
	case ImageScreenshot:
		files = []string{b.ScreenshotPath, b.IconPath}
		if b.ScreenshotPath != "" {
			files = append(files, strings.TrimSuffix(b.ScreenshotPath, ".jpg")+".json")
		}
		b.ScreenshotPath, b.IconPath = "", ""
	default:
		return nil, fmt.Errorf("unknown image kind %q", kind)
	}
	if b.ImageChoice == kind {
		b.ImageChoice = ImageAuto
	}

	var nonEmpty []string
	for _, f := range files {
		if f != "" {
			nonEmpty = append(nonEmpty, f)
		}
	}
	return nonEmpty, nil
}

// RemoveFiles deletes files, all of which must be inside dir (anything else is
// refused, whatever the database said). Files that are already gone are fine.
// It attempts every file and returns the first problem.
func RemoveFiles(dir string, files []string) error {
	root, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	var first error
	for _, f := range files {
		abs, err := filepath.Abs(f)
		if err == nil {
			var rel string
			rel, err = filepath.Rel(root, abs)
			if err == nil && (rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
				err = fmt.Errorf("refusing to delete %s: outside %s", f, dir)
			}
		}
		if err == nil {
			if err = os.Remove(abs); os.IsNotExist(err) {
				err = nil
			}
		}
		if err != nil && first == nil {
			first = err
		}
	}
	return first
}

// LoadedShort is when the card was loaded as a short date ("Oct 15", or with
// the year if it isn't this year); empty if it was never loaded.
func (b *Bookmark) LoadedShort() string {
	if !b.CardLoaded() {
		return ""
	}
	t := b.OGFetchedAt.Local()
	if t.Year() == time.Now().Year() {
		return t.Format("Jan 2")
	}
	return t.Format("Jan 2, 2006")
}

// CardLoaded reports whether the card has been loaded from the site (as
// opposed to just having some stored values, eg. from before loads were
// timestamped). A load can succeed with only some of the tags present, so this
// says nothing about which parts of the card exist; see CardMissing.
func (b *Bookmark) CardLoaded() bool {
	return b.OGFetchedAt.Unix() > 0
}

// CardMissing names the text parts of the card the site didn't provide, for
// presenting partial loads: "title" and "description". A missing site name isn't
// listed because the domain stands in for it, and neither is the image: whether
// we hold a file says nothing about whether the site had one (it may have been
// deleted, or failed to download, or we may be showing the screenshot).
func (b *Bookmark) CardMissing() []string {
	var missing []string
	if b.OGTitle == "" {
		missing = append(missing, "title")
	}
	if b.OGDescription == "" {
		missing = append(missing, "description")
	}
	return missing
}

// CardTitle is the title the site gave itself, falling back to ours and then the domain.
func (b *Bookmark) CardTitle() string {
	switch {
	case b.OGTitle != "":
		return b.OGTitle
	case b.Title != "":
		return b.Title
	}
	return b.Domain()
}

// Domain is the bookmark's host without a leading "www.".
func (b *Bookmark) Domain() string {
	u, err := url.Parse(b.URL)
	if err != nil || u.Hostname() == "" {
		return b.URL
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

// CardSource is the name shown under the card: the site's name if it gave us
// one, otherwise its domain.
func (b *Bookmark) CardSource() string {
	if b.SiteName != "" {
		return b.SiteName
	}
	return b.Domain()
}

func (b *Bookmark) preSave() {
	b.DescriptionRendered = mtr.RenderMarkdown(b.Description)
	b.URL = strings.TrimSpace(b.URL)
	if !b.UpdatedAt.IsZero() {
		b.UpdatedAt = time.Now()
	}
	if b.ID == "" {
		b.ID = uuid.New().String()
	}
}

type BookmarkService struct {
	db                db.DB
	screenshotService *ScreenshotService
}

func NewBookmarkService(db db.DB) *BookmarkService {
	return &BookmarkService{db: db}
}

func (s *BookmarkService) SetScreenshotService(ss *ScreenshotService) {
	s.screenshotService = ss
}

// createIconIfNeeded creates an icon for a bookmark if it has a screenshot but no icon
func (s *BookmarkService) createIconIfNeeded(b *Bookmark) {
	if s.screenshotService == nil || b.ScreenshotPath == "" || b.IconPath != "" {
		return
	}

	// Generate icon path based on screenshot path
	iconPath := strings.TrimSuffix(b.ScreenshotPath, ".jpg") + "-256x160.jpg"

	// Try to create the icon
	if err := s.screenshotService.ResizeScreenshot(b.ScreenshotPath, iconPath, 256, 160); err != nil {
		// Log the error but don't fail the save operation
		return
	}

	// Set the icon path if creation was successful
	b.IconPath = iconPath
}

func (s *BookmarkService) DeleteByID(id string) error {
	_, err := s.db.Exec("DELETE FROM bookmark WHERE id=?", id)
	return err
}

func (s *BookmarkService) GetByID(id string) (*Bookmark, error) {
	var b Bookmark
	if err := s.db.Get(&b, `SELECT * FROM bookmark WHERE id=?`, id); err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *BookmarkService) GetByURL(url string) (*Bookmark, error) {
	var b Bookmark
	if err := s.db.Get(&b, `SELECT * FROM bookmark WHERE url=?`, url); err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *BookmarkService) Select(query string, args ...interface{}) ([]Bookmark, error) {
	var bookmarks []Bookmark
	fullQuery := "SELECT * FROM bookmark " + query
	if err := s.db.Select(&bookmarks, fullQuery, args...); err != nil {
		return nil, err
	}
	return bookmarks, nil
}

func (s *BookmarkService) Insert(b *Bookmark) error {
	q := `INSERT INTO bookmark
		(id, url, title, description, description_rendered, screenshot_path, icon_path, site_name, og_image_path,
		 og_title, og_description, og_fetched_at, image_choice, published) VALUES
		(:id, :url, :title, :description, :description_rendered, :screenshot_path, :icon_path, :site_name, :og_image_path,
		 :og_title, :og_description, :og_fetched_at, :image_choice, :published);
	`
	b.preSave()
	s.createIconIfNeeded(b)

	return db.With(s.db, func(tx *sqlx.Tx) error {
		stmt, err := tx.PrepareNamed(q)
		if err != nil {
			return err
		}
		defer stmt.Close()

		_, err = stmt.Exec(b)
		if err != nil {
			return fmt.Errorf("insert %w", err)
		}

		return nil
	})
}

func (s *BookmarkService) Save(b *Bookmark) error {
	if b.ID == "" {
		return s.Insert(b)
	}

	b.preSave()
	s.createIconIfNeeded(b)

	return db.With(s.db, func(tx *sqlx.Tx) error {
		q := `UPDATE bookmark SET
			url=:url, title=:title, description=:description, description_rendered=:description_rendered,
			screenshot_path=:screenshot_path, icon_path=:icon_path, site_name=:site_name,
			og_image_path=:og_image_path, og_title=:og_title, og_description=:og_description,
			og_fetched_at=:og_fetched_at, image_choice=:image_choice, published=:published, updated_at=:updated_at,
			published_at=:published_at
		WHERE id=:id`
		update, err := tx.PrepareNamed(q)
		if err != nil {
			return err
		}
		defer update.Close()
		_, err = update.Exec(b)

		return err
	})
}

func (s *BookmarkService) Search(query string, pageSize, offset int) ([]Bookmark, error) {
	var ids []string
	searchq := fmt.Sprintf(`SELECT id FROM bookmark_fts WHERE published > 0 AND bookmark_fts
		MATCH ? ORDER BY rank LIMIT %d OFFSET %d`, pageSize, offset)

	if err := s.db.Select(&ids, searchq, query); err != nil {
		return nil, err
	}

	if len(ids) == 0 {
		return []Bookmark{}, nil
	}

	// Convert to interface{} slice for sqlx.In
	var idArgs []interface{}
	for _, id := range ids {
		idArgs = append(idArgs, id)
	}

	q, args, err := sqlx.In(`SELECT * FROM bookmark WHERE id IN (?) ORDER BY created_at DESC`, idArgs)
	if err != nil {
		return nil, err
	}

	var bookmarks []Bookmark
	if err := s.db.Select(&bookmarks, q, args...); err != nil {
		return nil, err
	}

	return bookmarks, nil
}

func (s *BookmarkService) SearchCount(query string) (int, error) {
	var count int
	countq := `SELECT count(*) FROM bookmark_fts WHERE published > 0 AND bookmark_fts MATCH ?`
	if err := s.db.Get(&count, countq, query); err != nil {
		return 0, err
	}
	return count, nil
}

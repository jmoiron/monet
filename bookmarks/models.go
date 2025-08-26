package bookmarks

import (
	"fmt"
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
	Published           int
	CreatedAt           time.Time `db:"created_at"`
	UpdatedAt           time.Time `db:"updated_at"`
	PublishedAt         time.Time `db:"published_at"`
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
		(id, url, title, description, description_rendered, screenshot_path, icon_path, published) VALUES
		(:id, :url, :title, :description, :description_rendered, :screenshot_path, :icon_path, :published);
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

		tx.Exec(`insert into bookmark_fts(bookmark_fts) values ('rebuild')`)

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
			screenshot_path=:screenshot_path, icon_path=:icon_path, published=:published, updated_at=:updated_at,
			published_at=:published_at
		WHERE id=:id`
		update, err := tx.PrepareNamed(q)
		if err != nil {
			return err
		}
		defer update.Close()
		_, err = update.Exec(b)

		// attempt to re-build the full text search index, which seems to
		// get corrupted by our update triggers for some reason
		tx.Exec(`insert into bookmark_fts(bookmark_fts) values ('rebuild')`)

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

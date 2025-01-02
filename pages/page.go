package pages

import (
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/sqlx"
)

var pageMigrations = monarch.Set{
	Name: "page",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS page (
				id integer PRIMARY KEY,
				url text,
				content text,
				content_rendered text,
				created_at datetime DEFAULT (strftime('%s', 'now')),
				updated_at datetime DEFAULT (strftime('%s', 'now'))
			);`,
			Down: `DROP TABLE page;`,
		},
	},
}

type Page struct {
	ID              int
	URL             string
	Title           string
	Content         string
	ContentRendered string    `db:"content_rendered"`
	CreatedAt       time.Time `db:"created_at"`
	UpdatedAt       time.Time `db:"updated_at"`
}

func (p *Page) preSave() {
	p.ContentRendered = mtr.RenderMarkdown(p.Content)
	p.URL = strings.TrimPrefix(p.URL, "/")
	if !p.UpdatedAt.IsZero() {
		p.UpdatedAt = time.Now()
	}
}

type PageService struct {
	db db.DB
}

func NewPageService(db db.DB) *PageService {
	return &PageService{db: db}
}

func (s *PageService) DeleteByID(id int) error {
	_, err := s.db.Exec("DELETE FROM page WHERE id=?", id)
	return err
}

func (s *PageService) GetByURL(url string) (*Page, error) {
	var p Page
	if err := s.db.Get(&p, `SELECT * FROM page WHERE url=?`, url); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *PageService) GetByID(id int) (*Page, error) {
	var p Page
	if err := s.db.Get(&p, `SELECT * FROM page WHERE id=?`, id); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *PageService) Insert(p *Page) error {
	q := `INSERT INTO page
		(url, content, content_rendered) VALUES
		(:url, :content, :content_rendered);
	`
	p.preSave()

	return db.With(s.db, func(tx *sqlx.Tx) error {
		stmt, err := tx.PrepareNamed(q)
		if err != nil {
			return err
		}
		defer stmt.Close()

		res, err := stmt.Exec(p)
		if err != nil {
			return fmt.Errorf("insert %w", err)
		}

		// we need to get the id out to add the tags to the join table
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("lastInsertID %w", err)
		}
		p.ID = int(id)

		return nil
	})

}

// Save p to the databse.  If p's ID is < 0, a new page is created, and
// p's ID is modified to suit the new id in the db.
func (s *PageService) Save(p *Page) error {
	if p.ID <= 0 {
		return s.Insert(p)
	}

	p.preSave()

	return db.With(s.db, func(tx *sqlx.Tx) error {
		q := `UPDATE page SET
			url=:url, content=:content, content_rendered=:content_rendered,
			updated_at=:updated_at
		WHERE id=:id`
		update, err := tx.PrepareNamed(q)
		if err != nil {
			return err
		}
		defer update.Close()
		_, err = update.Exec(p)
		return err
	})
}

// InsertArchive inserts a page from an archive
// NOTE: archived pages do not have timestamps or titles so we're ignoring those
func (s *PageService) InsertArchive(p *Page) error {
	q := `INSERT INTO page (url, content, content_rendered) VALUES
			(:url, :content, :content_rendered);`

	stmt, err := s.db.PrepareNamed(q)
	if err != nil {
		return err
	}

	_, err = stmt.Exec(p)
	return err
}

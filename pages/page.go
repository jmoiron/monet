package pages

import (
	"time"

	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/monarch"
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

type PageService struct {
	db db.DB
}

func NewPageService(db db.DB) *PageService {
	return &PageService{db: db}
}

func (s *PageService) GetByURL(url string) (*Page, error) {
	var p Page
	if err := s.db.Get(&p, `SELECT * FROM page WHERE url=?`, url); err != nil {
		return nil, err
	}
	return &p, nil
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

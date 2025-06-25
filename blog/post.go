package blog

import (
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/sqlx"
)

var postMigrations = monarch.Set{
	Name: "post",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS post (
				id INTEGER PRIMARY KEY,
				title TEXT,
				slug TEXT,
				content TEXT DEFAULT '',
				content_rendered TEXT DEFAULT '',
				created_at datetime DEFAULT (datetime('now')),
				updated_at datetime DEFAULT (datetime('now')),
				published_at datetime DEFAULT 0,
				published INTEGER DEFAULT 0
			);`,
			Down: `DROP TABLE post;`,
		}, {
			Up: `CREATE VIRTUAL TABLE post_fts USING fts5(
				id, title, slug, content, published,
				content='post',
				content_rowid='id',
				tokenize="trigram"
			)`,
			Down: `drop table post_fts;`,
		}, {
			Up:   `INSERT INTO post_fts SELECT id, title, slug, content, published FROM post;`,
			Down: `DELETE FROM post_fts;`,
		}, {
			Up: `CREATE TRIGGER post_i AFTER INSERT ON post BEGIN
				INSERT INTO post_fts (id, title, slug, content, published) VALUES
					(new.id, new.title, new.slug, new.content, new.published);
			 END;`,
			Down: `DROP TRIGGER post_i;`,
		}, {
			Up: `CREATE TRIGGER post_d AFTER DELETE ON post BEGIN
				INSERT INTO post_fts (post_fts, id, title, slug, content, published) VALUES
					('delete', old.id, old.title, old.slug, old.content, old.published);
			END`,
			Down: `DROP TRIGGER post_d;`,
		}, {
			// delete + insert
			Up: `CREATE TRIGGER post_u AFTER UPDATE ON post BEGIN
				INSERT INTO post_fts (post_fts, id, title, slug, content, published) VALUES
					('delete', old.id, old.title, old.slug, old.content, old.published);
				INSERT INTO post_fts (id, title, slug, content, published) VALUES
					(new.id, new.title, new.slug, new.content, new.published);
			END`,
			Down: `DROP TRIGGER post_u;`,
		}, {
			Up:   `ALTER TABLE post ADD COLUMN og_description text default '';`,
			Down: `ALTER TABLE post DROP COLUMN og_description;`,
		}, {
			Up:   `ALTER TABLE post ADD COLUMN og_image text default '';`,
			Down: `ALTER TABLE post DROP COLUMN og_image;`,
		},
	},
}

var postTagMigrations = monarch.Set{
	Name: "post_tag",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS post_tag (
				post_id INTEGER,
				tag TEXT,
				FOREIGN KEY (post_id) REFERENCES post(id)
			);`,
			Down: `DROP TABLE post_tag;`,
		},
	},
}

// A Post is an entry in a blog
type Post struct {
	ID              uint64
	Title           string
	Slug            string
	Content         string
	ContentRendered string    `db:"content_rendered"`
	CreatedAt       time.Time `db:"created_at"`
	UpdatedAt       time.Time `db:"updated_at"`
	PublishedAt     time.Time `db:"published_at"`
	Published       int
	Tags            []string `db:"-"`
	// OpenGraph Tags
	OgDescription string `db:"og_description"`
	OgImage       string `db:"og_image"`
	// test usage
	now func() time.Time
}

// A postTag associates a post with a tag
type postTag struct {
	PostID uint64 `db:"post_id"`
	Tag    string
}

type PostService struct {
	db db.DB
}

// NewPostService returns a cursor for Posts.
func NewPostService(db db.DB) *PostService {
	return &PostService{db: db}
}

// Get a post by its id.
func (s *PostService) Get(id int) (*Post, error) {
	var p Post
	err := s.db.Get(&p, "SELECT * FROM post WHERE id=?", id)
	if err != nil {
		return nil, err
	}
	if err = s.loadTags(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

// GetSlug gets a post by its slug.
func (s *PostService) GetSlug(slug string) (*Post, error) {
	var p Post
	err := s.db.Get(&p, `SELECT * FROM post WHERE slug=?`, slug)
	if err != nil {
		return nil, err
	}
	if err = s.loadTags(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

// Select multiple posts via a query.
func (s *PostService) Select(where string, args ...interface{}) ([]*Post, error) {
	q := fmt.Sprintf(`SELECT * FROM post %s`, where)
	var posts []*Post
	err := s.db.Select(&posts, q, args...)

	if err != nil {
		return nil, err
	}
	err = s.loadTags(posts...)
	if err != nil {
		return nil, err
	}

	return posts, nil
}

// loadTags fetches tags for each post and sets them to that post.
func (s *PostService) loadTags(posts ...*Post) error {
	var ids []int
	var postMap = make(map[uint64]*Post)

	for i, p := range posts {
		ids = append(ids, int(p.ID))
		postMap[p.ID] = posts[i]
	}

	q, args, err := sqlx.In(`SELECT * FROM post_tag WHERE post_id IN (?) ORDER BY post_id;`, ids)
	if err != nil {
		return err
	}

	var tags []postTag
	err = s.db.Select(&tags, q, args...)
	if err != nil {
		return err
	}

	// we can be more clever but lets not bother
	for _, tag := range tags {
		post := postMap[tag.PostID]
		post.Tags = append(post.Tags, tag.Tag)
	}

	return nil
}

// Save p to the database.  If p's ID is 0, it is created with a new
// ID, otherwise it is updated.  Even if the insertion or the update
// is a failure, preSave routines that modify p will run.
func (s *PostService) Save(p *Post) error {

	if p.ID == 0 {
		return s.Insert(p)
	}

	p.preSave()

	return db.With(s.db, func(tx *sqlx.Tx) error {
		q := `UPDATE post SET
		title=:title, slug=:slug, content=:content, content_rendered=:content_rendered,
		updated_at=:updated_at, published_at=:published_at, published=:published
	WHERE id=:id`
		update, err := tx.PrepareNamed(q)
		if err != nil {
			return err
		}
		defer update.Close()
		_, err = update.Exec(p)
		if err != nil {
			return err
		}
		err = updateTags(tx, p)
		if err != nil {
			return err
		}

		// attempt to re-build the full text search index, which seems to
		// get corrupted by our update triggers for some reason
		tx.Exec(`insert into post_fts(post_fts) values ('rebuild')`)

		return nil
	})

}

// Insert p into the database db.  If successful, p.ID will be set to the
// auto incremented ID provided by the database.
func (s *PostService) Insert(p *Post) error {
	q := `INSERT INTO post
	(title, slug, content, content_rendered, published) VALUES
	(:title, :slug, :content, :content_rendered, :published);`

	p.preSave()

	return db.With(s.db, func(tx *sqlx.Tx) error {
		stmt, err := tx.PrepareNamed(q)
		if err != nil {
			return err
		}
		defer stmt.Close()

		res, err := stmt.Exec(p)
		if err != nil {
			return err
		}
		// we need to get the id out to add the tags to the join table
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		p.ID = uint64(id)

		err = updateTags(tx, p)
		if err != nil {
			return err
		}
		return nil
	})
}

// InsertArchive inserts a post as an archival post, skipping the pre-save
func (s *PostService) InsertArchive(p *Post) error {
	// if CreatedAt is set, then do a full insert
	q := `INSERT INTO post
		(title, slug, content, content_rendered, created_at, updated_at, published_at, published) values
		(:title, :slug, :content, :content_rendered, :created_at, :updated_at, :published_at, :published);`

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
		p.ID = uint64(id)

		err = updateTags(tx, p)
		if err != nil {
			return fmt.Errorf("updateTags %w", err)
		}
		return nil
	})
}

// updateTags updates the tags for post p.  p should have a non-zero ID.
// updateTags does not commit or rollback the passed in transaction.
func updateTags(tx *sqlx.Tx, p *Post) error {
	if _, err := tx.Exec(`DELETE FROM post_tag WHERE post_id=?`, p.ID); err != nil {
		return err
	}

	// if there are no tags, we're done
	if len(p.Tags) == 0 {
		return nil
	}

	var (
		tags []string
		args []any
	)

	for _, tag := range p.Tags {
		tags = append(tags, "(?, ?)")
		args = append(args, p.ID, tag)
	}

	q := fmt.Sprintf(`INSERT INTO post_tag (post_id, tag) VALUES %s`,
		strings.Join(tags, ", "))

	_, err := tx.Exec(q, args...)
	return err
}

// preSave is run prior to saving, ensuring that certain fields have
// appropriate defaults when "empty" and others get updated
func (p *Post) preSave() {
	// render the content to a cached content field
	p.ContentRendered = mtr.RenderMarkdown(p.Content)
	// create a slug
	p.Slug = db.Slugify(p.Title)

	if p.now == nil {
		p.now = time.Now
	}
	now := p.now()

	// FIXME: fix
	p.UpdatedAt = now
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}

}

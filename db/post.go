package db

import (
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/monet/template"
	"github.com/jmoiron/sqlx"
)

// A Post is an entry in a blog
type Post struct {
	ID              uint64
	Title           string
	Slug            string
	Content         string
	ContentRendered string `db:"content_rendered"`
	Summary         string
	Timestamp       uint64
	Published       int
	Tags            []string `db:"-"`
}

// A postTag associates a post with a tag
type postTag struct {
	PostID uint64 `db:"post_id"`
	Tag    string
}

type PostService struct {
	db DB
}

// NewPostService returns a cursor for Posts.
func NewPostService(db DB) *PostService {
	return &PostService{db}
}

// Get a post by its id.
func (s *PostService) Get(id int) (*Post, error) {
	var p Post
	err := s.db.Get(&p, "SELECT * FROM post WHERE id=?", id)
	if err != nil {
		return nil, err
	}
	err = s.loadTags([]*Post{&p})
	if err != nil {
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
	err = s.loadTags(posts)
	if err != nil {
		return nil, err
	}

	return posts, nil
}

// loadTags fetches tags for each post and sets them to that post.
func (s *PostService) loadTags(posts []*Post) error {
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

// Save p to the database db.  If its ID is 0, it is created with a
// new ID, otherwise it's updated.  Even if the insertion or update
// is a failure, p will still run a preSave routine that may modify
// it.
func (p *Post) Save(db DB) error {
	// run preSave;  this runs even if
	p.preSave()

	// if the ID is zero, then insert
	if p.ID == 0 {
		return p.Insert(db)
	}

	tx, err := db.Beginx()
	if err != nil {
		return err
	}

	return With(tx, func(tx *sqlx.Tx) error {
		q := `UPDATE post SET
		title=:title, slug=:slug, content=:content, content_rendered=:content_rendered,
		summary=:summary, timestamp=:timestamp, published=:published
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
		err = p.updateTags(tx)
		if err != nil {
			return err
		}

		return nil
	})

}

// Insert p into the database db.  If successful, p.ID will be set to the
// auto incremented ID provided by the database.
func (p *Post) Insert(db DB) error {
	q := `INSERT INTO post
	(title, slug, content, content_rendered, summary, timestamp, published) VALUES
	(:title, :slug, :content, :content_rendered, :summary, :timestamp, :published);`

	// TODO: tx, update tags

	tx, err := db.Beginx()
	if err != nil {
		return err
	}

	return With(tx, func(tx *sqlx.Tx) error {
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

		err = p.updateTags(tx)
		if err != nil {
			return err
		}
		return nil

	})
}

// preSave is run prior to saving, ensuring that certain fields have
// appropriate defaults when "empty" and others get updated
func (p *Post) preSave() {
	// render the content to a cached content field
	p.ContentRendered = template.RenderMarkdown(p.Content)

	// create a slug
	p.Slug = Slugify(p.Title)

	// TODO: summarize if missing?

	// set the timestamp to now if it's unset
	if p.Timestamp == 0 {
		p.Timestamp = uint64(time.Now().Unix())
	}
}

func (p *Post) updateTags(tx *sqlx.Tx) error {
	_, err := tx.Exec(`DELETE FROM post_tag WHERE post_id=?`, p.ID)
	if err != nil {
		return err
	}

	var tags []string
	var args []interface{}
	for _, tag := range p.Tags {
		tags = append(tags, "(?, ?)")
		args = append(args, p.ID)
		args = append(args, tag)
	}
	q := fmt.Sprintf(`INSERT INTO post_tag (post_id, tag) VALUES %s`,
		strings.Join(tags, ", "))
	_, err = tx.Exec(q, args...)
	return err

}

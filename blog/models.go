package blog

import (
	"errors"
	"time"

	"github.com/jmoiron/monet/monarch"
	"github.com/jmoiron/monet/template"
	"github.com/jmoiron/sqlx"
)

// A Post in a blog.
type Post struct {
	ID              int
	Title           string
	Slug            string
	Content         string
	ContentRendered string `db:content_rendered`
	CreatedAt       uint64
	UpdatedAt       uint64
	PublishedAt     uint64
	Published       int
}

// A Manager is a datalayer for the blog app.  It persists and fetches data
// and provides a high level interface with which to build the app.
type Manager struct {
	db monarch.DB
}

// Save p to the database.
func (m *Manager) Save(p *Post) error {
	p.ContentRendered = template.RenderMarkdown(p.Content)
	if p.ID == 0 {
		return m.create(p)
	}
	return m.update(p)
}

// create post p in the database.  p's id must be invalid (zero).
func (m *Manager) create(p *Post) error {
	if p.ID != 0 {
		return errors.New("cannot create post with id")
	}

	// timestamps all get set properly by default.
	query, args, err := sqlx.Named(`
	INSERT INTO post
		(title, slug, content, content_rendered, published) VALUES
		(:title, :slug, :content, :content_rendered, :published);`,
		p)

	if err != nil {
		return err
	}

	_, err = m.db.Exec(query, args...)
	return err
}

// update p in the database.  p must have a valid (non-zero) id.
func (m *Manager) update(p *Post) error {
	if p.ID == 0 {
		return errors.New("cannot update post without id")
	}

	// adjust UpdatedAt timestamp
	p.UpdatedAt = uint64(time.Now().Unix())

	query, args, err := sqlx.Named(`
	UPDATE post SET
		title=:title, slug=:slug, content=:content, content_rendered=:content_rendered,
		updated_at=:updated_at, published=:published) WHERE id=:id;`, p)

	if err != nil {
		return err
	}

	_, err = m.db.Exec(query, args...)
	return err
}

// ByID loads a post by ID.
func (m *Manager) ByID(id int) (*Post, error) {
	query := `SELECT * FROM post WHERE id=?;`
	var p Post
	err := m.db.Get(&p, query, id)
	return &p, err
}

// BySlug loads a post by its slug.
func (m *Manager) BySlug(slug string) (*Post, error) {
	query := `SELECT * FROM post WHERE slug=?;`
	var p Post
	err := m.db.Get(&p, query, slug)
	return &p, err
}

func (m *Manager) Latest() (*Post, error) {
	query := `SELECT * FROM post ORDER BY published_at DESC LIMIT 1;`
	var p Post
	err := m.db.Get(&p, query)
	return &p, err
}

/*
// Flatpage Model
type Page struct {
	Id              bson.ObjectId `bson:"_id,omitempty"`
	Url             string
	Content         string
	ContentRendered string
}

type Entry struct {
	Id              bson.ObjectId `bson:"_id,omitempty"`
	SourceId        string
	Url             string
	Type            string
	Title           string
	Data            string
	SummaryRendered string
	ContentRendered string
	Timestamp       uint64
}

// Post Implementation

// Instantiate a post object from POST parameters
func (p *Post) FromParams(params map[string]string) error {
	if len(params["id"]) > 0 {
		p.Id = bson.ObjectIdHex(params["id"])
	}
	p.Content = params["content"]
	p.Title = params["title"]
	p.Slug = params["slug"]
	if len(params["timestamp"]) > 0 {
		ts, err := strconv.ParseInt(params["timestamp"], 10, 0)
		if err != nil {
			return err
		}
		p.Timestamp = uint64(ts)
	} else {
		p.Timestamp = uint64(time.Now().Unix())
	}
	p.Published, _ = strconv.Atoi(params["published"])
	p.ContentRendered = template.RenderMarkdown(p.Content)
	return nil
}

func (p *Post) NaturalTime() string {
	return app.FmtTimestamp(p.Timestamp)
}

// Page Implementation

func (p *Page) PreSave() {
	p.ContentRendered = template.RenderMarkdown(p.Content)
	p.Url = strings.TrimLeft(p.Url, "/")
}

// Instantiate a Page object from POST parameters
func (p *Page) FromParams(params map[string]string) error {
	if len(params["id"]) > 0 {
		p.Id = bson.ObjectIdHex(params["id"])
	}
	p.Content = params["content"]
	p.Url = params["url"]
	return nil
}

// Entry implementation

func (e *Entry) Indexes() [][]string {
	return [][]string{
		[]string{"checksum"},
		[]string{"timestamp"},
	}
}

func (e *Entry) Sorting() string    { return "-timestamp" }
func (e *Entry) Collection() string { return "stream" }
func (e *Entry) PreSave()           {}
func (e *Entry) Unique() bson.M {
	if len(e.Id) > 0 {
		return bson.M{"_id": e.Id}
	}
	return bson.M{"slug": e.SourceId, "type": e.Type}
}

func (e *Entry) SummaryRender() string {
	if len(e.SummaryRendered) > 0 && !conf.Config.Debug {
		return e.SummaryRendered
	}
	var ret string
	var data obj
	b := []byte(e.Data)
	json.Unmarshal(b, &data)

	template_name := fmt.Sprintf("blog/stream/%s-summary.mandira", e.Type)

	if e.Type == "twitter" {
		ret = template.Render(template_name, obj{"Entry": e, "Tweet": data["tweet"]})
	} else if e.Type == "github" {
		event := data["event"].(map[string]interface{})
		var hash string
		if event["id"] != nil {
			hash = event["id"].(string)[:8]
		} else if event["sha"] != nil {
			hash = event["sha"].(string)[:8]
		} else {
			hash = "unknown"
		}
		eventType := event["event"].(string)
		isCommit := eventType == "commit"
		isCreate := eventType == "create"
		isFork := eventType == "fork"
		ret = template.Render(template_name, obj{
			"Entry":    e,
			"Event":    event,
			"Hash":     hash,
			"IsCommit": isCommit,
			"IsCreate": isCreate,
			"IsFork":   isFork,
		})
	} else if e.Type == "bitbucket" {
		// TODO: check username (author) against configured bitbucket username
		update := data["update"].(map[string]interface{})
		revision := fmt.Sprintf("#%d", update["revision"].(float64))
		var repository obj
		if data["repository"] != nil {
			repository = data["repository"].(obj)
		}
		ret = template.Render(template_name, obj{
			"Entry":      e,
			"Data":       data,
			"Update":     update,
			"Repository": repository,
			"Revision":   revision})
	}
	e.SummaryRendered = ret
	if !conf.Config.Debug {
		db.Upsert(e)
	}
	return ret
}
*/

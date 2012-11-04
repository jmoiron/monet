package blog

import (
	"encoding/json"
	"fmt"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/conf"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/template"
	"labix.org/v2/mgo"
	"labix.org/v2/mgo/bson"
	"strconv"
	"strings"
	"time"
)

type obj map[string]interface{}

// Blog Post Model
type Post struct {
	Id              bson.ObjectId `bson:"_id,omitempty"`
	Title           string
	Slug            string
	Content         string
	ContentRendered string
	Summary         string
	Tags            []string
	Timestamp       uint64
	Published       int
}

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

func (p *Post) Collection() string { return "posts" }
func (p *Post) Sorting() string    { return "-timestamp" }

func (p *Post) Indexes() [][]string {
	return [][]string{
		[]string{"slug"},
		[]string{"timestamp"},
	}
}

func (p *Post) Unique() bson.M {
	if len(p.Id) > 0 {
		return bson.M{"_id": p.Id}
	}
	return bson.M{"slug": p.Slug}
}

func (p *Post) PreSave() {
	p.ContentRendered = template.RenderMarkdown(p.Content)
}

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

func (p *Page) Indexes() [][]string { return [][]string{[]string{"url"}} }
func (p *Page) Collection() string  { return "pages" }

func (p *Page) Unique() bson.M {
	if len(p.Id) > 0 {
		return bson.M{"_id": p.Id}
	}
	return bson.M{"url": p.Url}
}

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

// A shortcut to return the latest blog post (or nil if there aren't any)
func LatestPost() *Post {
	var post *Post
	err := db.Latest(post, bson.M{}).One(&post)
	if err != nil && err != mgo.ErrNotFound {
		panic(err)
	}
	if err == mgo.ErrNotFound {
		return nil
	}
	return post
}

// A shortcut to return the page for a given url (or nil if there isn't one)
func GetPage(url string) *Page {
	var page *Page
	err := db.Find(page, bson.M{"url": url}).One(&page)
	if err != nil && err != mgo.ErrNotFound {
		panic(err)
	}
	if err == mgo.ErrNotFound {
		return nil
	}
	return page
}

func init() {
	db.Register(&Post{})
	db.Register(&Page{})
	db.Register(&Entry{})
}

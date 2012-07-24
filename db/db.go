package db

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"github.com/jmoiron/monet/conf"
	"github.com/jmoiron/monet/template"
	"io"
	"labix.org/v1/mgo"
	"labix.org/v1/mgo/bson"
	"strconv"
	"strings"
	"time"
)

var Session *mgo.Session
var Db *mgo.Database

type dict map[string]interface{}

type Model interface {
	Update() error
	FromParams(map[string]string) error
}

// -- models --
type Post struct {
	Id              bson.ObjectId "_id"
	Title           string
	Slug            string
	Content         string
	ContentRendered string
	Summary         string
	Tags            []string
	Timestamp       uint64
	Published       int
}

type Note struct {
	Id      bson.ObjectId "_id"
	Title   string
	Slug    string
	Content string
}

type User struct {
	Id       bson.ObjectId "_id"
	Username string
	Password string
}

type Page struct {
	Id              bson.ObjectId "_id"
	Url             string
	Content         string
	ContentRendered string
}

type StreamEntry struct {
	Id              bson.ObjectId "_id"
	SourceId        string
	Url             string
	Type            string
	Title           string
	Data            string
	SummaryRendered string
	ContentRendered string
	Timestamp       uint64
}

// -- cursors --
type Cursor struct{ C *mgo.Collection }
type PostCursor struct{ Cursor }
type UserCursor struct{ Cursor }
type NoteCursor struct{ Cursor }
type PageCursor struct{ Cursor }
type StreamCursor struct{ Cursor }

func Posts() *PostCursor {
	p := new(PostCursor)
	p.C = Db.C("posts")
	return p
}

func Users() *UserCursor {
	u := new(UserCursor)
	u.C = Db.C("users")
	return u
}

func Notes() *NoteCursor {
	n := new(NoteCursor)
	n.C = Db.C("notes")
	return n
}

func Pages() *PageCursor {
	p := new(PageCursor)
	p.C = Db.C("pages")
	return p
}

func Entries() *StreamCursor {
	p := new(StreamCursor)
	p.C = Db.C("stream")
	return p
}

// -- Posts --

func FmtTimestamp(ts uint64) string {
	now := time.Now()
	ut := time.Unix(int64(ts), 0)
	if now.Year() == ut.Year() {
		return ut.Format("Jan _2")
	}
	return ut.Format("Jan _2 2006")
}

func (p Post) NaturalTime() string {
	return FmtTimestamp(p.Timestamp)
}

func (p *Post) Update() error {
	p.ContentRendered = template.RenderMarkdown(p.Content)
	if len(p.Id) > 0 {
		fmt.Println("Updating using _id", p.Id)
		Posts().C.Update(bson.M{"_id": p.Id}, p)
	} else {
		p.Id = bson.NewObjectId()
		_, err := Posts().C.Upsert(bson.M{"slug": p.Slug}, p)
		if err != nil {
			fmt.Println(err)
		}
	}
	return nil
}

func (p *Post) FromParams(params map[string]string) error {
	p.Content = params["content"]
	p.Title = params["title"]
	p.Slug = params["slug"]
	if len(params["timestamp"]) > 0 {
		ts, _ := strconv.ParseInt(params["timestamp"], 10, 0)
		p.Timestamp = uint64(ts)
	} else {
		p.Timestamp = uint64(time.Now().Unix())
	}
	if len(params["id"]) > 0 {
		p.Id = bson.ObjectIdHex(params["id"])
	}
	p.Published, _ = strconv.Atoi(params["published"])
	p.ContentRendered = template.RenderMarkdown(p.Content)
	return nil
}

func (p *PostCursor) Latest(query interface{}) *mgo.Query {
	return p.C.Find(query).Sort(bson.M{"timestamp": -1})
}

func (p *PostCursor) LatestPost() *Post {
	var post *Post
	err := p.Latest(bson.M{"published": 1}).One(&post)
	if err != nil && err != mgo.NotFound {
		panic(err)
	}
	if err == mgo.NotFound {
		fmt.Printf("Cannot find any documents.")
		return nil
	}
	return post
}

// -- Users --

func (u *UserCursor) Create(username, password string) error {
	hash := sha1.New()
	io.WriteString(hash, password)
	user := new(User)
	user.Username = username
	user.Password = fmt.Sprintf("%x", hash.Sum(nil))
	_, err := u.C.Upsert(bson.M{"username": username}, user)
	return err
}

func (u *UserCursor) Validate(username, password string) bool {
	hash := sha1.New()
	io.WriteString(hash, password)
	user := new(User)
	hashstr := fmt.Sprintf("%x", hash.Sum(nil))
	err := Users().C.Find(bson.M{"username": username}).One(&user)
	if err == mgo.NotFound {
		return false
	}
	if user.Password != hashstr {
		return false
	}
	return true
}

// pages

func (p *PageCursor) Get(url string) *Page {
	var page *Page
	err := p.C.Find(bson.M{"url": url}).One(&page)
	if err != nil && err != mgo.NotFound {
		panic(err)
	}
	if err == mgo.NotFound {
		return nil
	}
	return page
}

func (p *Page) FromParams(params map[string]string) error {
	if len(params["id"]) > 0 {
		p.Id = bson.ObjectIdHex(params["id"])
	}
	p.Content = params["content"]
	p.Url = params["url"]
	return nil
}

func (p *Page) Update() error {
	p.ContentRendered = template.RenderMarkdown(p.Content)
	p.Url = strings.TrimLeft(p.Url, "/")
	if len(p.Id) > 0 {
		Pages().C.Update(bson.M{"_id": p.Id}, p)
	} else {
		p.Id = bson.NewObjectId()
		Pages().C.Upsert(bson.M{"url": p.Url}, p)
	}
	return nil
}

// entries

func (s *StreamCursor) Latest(query interface{}) *mgo.Query {
	return s.C.Find(query).Sort(bson.M{"timestamp": -1})
}

func (e *StreamEntry) Update() error {
	if len(e.Id) > 0 {
		fmt.Println("Updating using _id", e.Id)
		Entries().C.Update(bson.M{"_id": e.Id}, e)
	} else {
		e.Id = bson.NewObjectId()
		_, err := Entries().C.Upsert(bson.M{"slug": e.SourceId, "type": e.Type}, e)
		if err != nil {
			fmt.Println(err)
		}
	}
	return nil
}

func (e StreamEntry) SummaryRender() string {
	if len(e.SummaryRendered) > 0 && !conf.Config.Debug {
		return e.SummaryRendered
	}
	var ret string
	var data dict
	b := []byte(e.Data)
	json.Unmarshal(b, &data)
	template_name := fmt.Sprintf("%s-summary.mustache", e.Type)
	if e.Type == "twitter" {
		ret = template.Render(template_name, dict{"Entry": e, "Tweet": data["tweet"]})
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
		ret = template.Render(template_name, dict{"Entry": e, "Event": event, "Hash": hash})
	} else if e.Type == "bitbucket" {
		ret = template.Render(template_name, dict{"Entry": e, "Data": data})
	}
	e.SummaryRendered = ret
	if !conf.Config.Debug {
		e.Update()
	}
	return ret
}

func Connect() {
	url := conf.Config.DbHostString()
	Session, err := mgo.Dial(url)
	if err != nil {
		panic(err)
	}
	Db = Session.DB(conf.Config.DbName)
	initCollection()

	fmt.Printf("Connected to mongodb on %s, using \"%s\"\n", url, conf.Config.DbName)
}

// if the collections aren't set up yet, initialize them by creating
// the indexes monet needs to run properly
func initCollection() {
	Db.C("posts").EnsureIndexKey([]string{"slug"})
	Db.C("posts").EnsureIndexKey([]string{"timestamp"})
	Db.C("users").EnsureIndexKey([]string{"username"})
	Db.C("pages").EnsureIndexKey([]string{"url"})
	Db.C("stream").EnsureIndexKey([]string{"checksum"})
	Db.C("stream").EnsureIndexKey([]string{"timestamp"})
}

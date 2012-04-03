package db

import (
    "fmt"
    "io"
    "strings"
    "monet/conf"
    "monet/template"
    "crypto/sha1"
    "launchpad.net/mgo"
    "launchpad.net/mgo/bson"
)

var Session *mgo.Session
var Db *mgo.Database

type Model interface{}

// -- models --
type Post struct {
    Title string
    Slug string
    Content string
    ContentRendered string
    Summary string
    Tags []string
    Timestamp uint64
    Published int
}

type Note struct {
    Title string
    Slug string
    Content string
}

type User struct {
    Username string
    Password string
}

type Page struct {
    Url string
    Content string
    ContentRendered string
}

// -- cursors --
type PostCursor struct { C *mgo.Collection }
type UserCursor struct { C *mgo.Collection }
type NoteCursor struct { C *mgo.Collection }
type PageCursor struct { C *mgo.Collection }

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

// -- Posts --

func (p *Post) Update() error {
    p.ContentRendered = template.RenderMarkdown(p.Content)
    Posts().C.Upsert(bson.M{"slug": p.Slug}, p)
    return nil
}

func (p *PostCursor) Latest(query interface{}) *mgo.Query {
    return p.C.Find(query).Sort(bson.M{"timestamp":0})
}

func (p *PostCursor) LatestPost() *Post {
    var post *Post
    err := p.Latest(bson.M{"published":1}).One(&post)
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

func (u *UserCursor) Create(username, password string ) error {
    hash := sha1.New()
    io.WriteString(hash, password)
    user := new(User)
    user.Username = username
    user.Password = fmt.Sprintf("%x", hash.Sum(nil))
    _,err := u.C.Upsert(bson.M{"username":username}, user)
    return err
}

func (u *UserCursor) Validate(username, password string) bool {
    hash := sha1.New()
    io.WriteString(hash, password)
    user := new(User)
    hashstr := fmt.Sprintf("%x", hash.Sum(nil))
    err := Users().C.Find(bson.M{"username":username}).One(&user)
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
    err := p.C.Find(bson.M{"url":url}).One(&page)
    if err != nil && err != mgo.NotFound {
        panic(err)
    }
    if err == mgo.NotFound {
        return nil
    }
    return page
}

func (p *Page) Update() error {
    p.ContentRendered = template.RenderMarkdown(p.Content)
    p.Url = strings.TrimLeft(p.Url, "/")
    Pages().C.Upsert(bson.M{"url": p.Url}, p)
    return nil
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
}

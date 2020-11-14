package blog

import (
	"fmt"
	"net/http"
	"path"
	"time"

	"github.com/gorilla/feeds"
	"github.com/gorilla/mux"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/monarch"
	"github.com/jmoiron/monet/sunrise"
	"github.com/jmoiron/monet/template"
	"github.com/jmoiron/sqlx"
	"labix.org/v2/mgo/bson"
)

type Blog struct {
	db *sqlx.DB

	FeedRSSURL  string
	FeedAtomURL string
}

// NewBlog instantiates a new Blog.
func NewBlog(db *sqlx.DB) *Blog {
	return &Blog{db: db}
}

// Attach the blog to r at base.
func (b *Blog) Attach(r *mux.Router, base string) error {
	get := r.PathPrefix(base).Methods("GET")

	get.HandlerFunc("/rss", b.rss)
	get.HandlerFunc("/atom", b.atom)
	get.HandlerFunc("/page/{page:[0-9]+}", b.page)
	get.HandlerFunc("/{slug:[^/]+}", b.detail)
	get.HandlerFunc("/", b.index)
	// web.Get(url+"stream/page/(\\d+)", streamPage)
	// web.Get(url+"stream/", streamIndex)

	b.FeedRSSURL = path.Join(base, "rss")
	b.FeedAtomURL = path.Join(base, "atom")
	return nil
}

// Migrate the blog backend.
// TODO: how do we "rely" on modules, like auth/users?
// Maybe it's built into sunrise?
func (b *Blog) Migrate() error {
	manager, err := monarch.NewManager(b.db)
	if err != nil {
		return nil
	}

	migrations := []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS post (
				id INTEGER PRIMARY KEY,
				title TEXT,
				slug TEXT,
				content TEXT DEFAULT '',
				content_rendered TEXT DEFAULT '',
				summary TEXT DEFAULT '',
				timestamp INTEGER DEFAULT (strftime('%s', 'now')),
				published INTEGER DEFAULT 0
			);`,
			Down: `DROP TABLE post;`,
		},
		{
			Up: `CREATE TABLE IF NOT EXISTS post_tag (
				post_id INTEGER,
				tag TEXT,
				FOREIGN KEY (post_id) REFERENCES post(id)
			);`,
			Down: `DROP TABLE post_tag;`,
		},
	}

	set := monarch.Set{Name: "blog", Migrations: migrations}
	return manager.Upgrade(set)
}

func (b *Blog) Admin() (sunrise.Admin, error) {
	return nil
}

func (b *Blog) rss(w *http.ResponseWriter, req *http.Request) {
	w.Header()["Content-Type"] = "application/xml"

	feed := _createFeed()
	if feed == nil {
		return "<!-- error -->"
	}
	text, err := feed.ToRss()
	if err != nil {
		fmt.Println(err)
		return "<!-- error -->"
	}
	return text
}

// Render the post, using the cached ContentRendered if available, or generating
// and re-saving it to the database if not
func RenderPost(post *Post) string {
	if len(post.ContentRendered) == 0 {
		db.Upsert(post)
	}
	return template.Render("blog/post.mandira", post)
}

// A Flatpage view.  Attach it via web.Get wherever you want flatpages to be available
func Flatpage(url string) string {
	p := GetPage(url)
	fmt.Printf("Got page %v for url %s\n", p, url)
	if p == nil {
		ctx.Abort(404, "Page not found")
		return ""
	}

	return template.Render("base.mandira", M{
		"body":        p.ContentRendered,
		"title":       "jmoiron.net",
		"description": "Blog and assorted media from Jason Moiron.",
	})
}

func Index() string {
	var post *Post
	var entry *Entry
	var posts []Post
	var entries []*Entry

	err := db.Latest(post, M{"published": 1}).Limit(7).All(&posts)
	if err != nil {
		fmt.Println(err)
	}
	err = db.Latest(entry, nil).Limit(4).All(&entries)
	if err != nil {
		fmt.Println(err)
	}

	post = &posts[0]
	return base.Render("index.mandira", M{
		"Post":        RenderPost(post),
		"Posts":       posts[1:],
		"Entries":     entries,
		"title":       "jmoiron.net",
		"description": post.Summary})
}

func blogIndex() string {
	return blogPage(ctx, "1")
}

func blogPage(page string) string {
	pn := app.PageNumber(page)
	perPage := 15
	paginator := app.NewPaginator(pn, perPage)
	paginator.Link = "/blog/page/"

	var post *Post
	var posts []Post
	// do a search, if required, of title and content
	var err error
	var numObjects int

	if len(ctx.Params["Search"]) > 0 {
		term := M{"$regex": ctx.Params["Search"]}
		search := M{"published": 1, "$or": []M{M{"title": term}, M{"content": term}}}
		err = db.Latest(post, search).Skip(paginator.Skip).Limit(perPage).All(&posts)
		numObjects, _ = db.Latest(post, search).Count()
	} else {
		err = db.Latest(post, M{"published": 1}).Skip(paginator.Skip).
			Limit(perPage).Iter().All(&posts)
		numObjects, _ = db.Find(post, M{"published": 1}).Count()
	}

	if err != nil {
		fmt.Println(err)
	}

	return base.Render("blog/index.mandira", M{
		"Rss":        RssHref,
		"Atom":       AtomHref,
		"Posts":      posts,
		"Pagination": paginator.Render(numObjects)}, ctx.Params)
}

func _createFeed() *feeds.Feed {
	var posts []Post
	var post *Post

	err := db.Latest(post, M{"published": 1}).Limit(10).Iter().All(&posts)
	if err != nil {
		fmt.Println(err)
		return nil
	}

	feed := &feeds.Feed{
		Title:       "jmoiron.net blog",
		Link:        &feeds.Link{Href: "http://jmoiron.net"},
		Description: "the blog of Jason Moiron, all thoughts his own",
		Author:      &feeds.Author{"Jason Moiron", "jmoiron@jmoiron.net"},
		Updated:     time.Now(),
	}

	for _, post := range posts {
		feed.Add(&feeds.Item{
			Title:       post.Title,
			Link:        &feeds.Link{Href: "http://jmoiron.net/blog/" + post.Slug + "/"},
			Description: post.ContentRendered,
			Created:     time.Unix(int64(post.Timestamp), 0),
		})
	}
	return feed
}

func blogDetail(slug string) string {
	var post = new(Post)
	err := db.Find(post, M{"slug": slug}).One(&post)
	if err != nil {
		fmt.Println(err)
		ctx.Abort(404, "Page not found")
		return ""
	}

	return template.Render("base.mandira", M{
		"Rss":         RssHref,
		"Atom":        AtomHref,
		"body":        RenderPost(post),
		"title":       post.Title,
		"description": post.Summary})
}

func streamIndex() string {
	return streamPage(ctx, "1")
}

func streamPage(page string) string {
	num := app.PageNumber(page)
	perPage := 25
	paginator := app.NewPaginator(num, perPage)
	paginator.Link = "/stream/page/"

	var entry *Entry
	var entries []*Entry

	// do a search, if required, of title and content
	var err error
	var numObjects int

	if len(ctx.Params["Search"]) > 0 {
		re := new(bson.RegEx)
		re.Pattern = ctx.Params["Search"]
		re.Options = "i"
		term := M{"$regex": re}
		search := M{"summaryrendered": term}
		//search := M{"$or": []M{M{"title": term}, M{"summaryrendered": term}}}
		err = db.Latest(entry, search).Skip(paginator.Skip).Limit(perPage).All(&entries)
		numObjects, _ = db.Latest(entry, search).Count()
	} else {
		err = db.Latest(entry, nil).Skip(paginator.Skip).Limit(perPage).Iter().All(&entries)
		numObjects, _ = db.Cursor(entry).Count()
	}

	if err != nil {
		fmt.Println(err)
	}

	return base.Render("blog/stream/index.mandira", M{
		"Entries":    entries,
		"Pagination": paginator.Render(numObjects),
		"title":      "Lifestream"}, ctx.Params)
}

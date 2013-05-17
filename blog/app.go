package blog

import (
	"fmt"
	"github.com/gorilla/feeds"
	"github.com/hoisie/web"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/template"
	"labix.org/v2/mgo/bson"
	"time"
)

var base = template.Base{Path: "base.mandira"}
var RssHref string
var AtomHref string

// Attach the blog app frontend
func Attach(url string) {
	web.Get(url+"blog/page/(\\d+)", blogPage)
	web.Get(url+"blog/([^/]+)/", blogDetail)
	web.Get(url+"blog/", blogIndex)
	web.Get(url+"stream/page/(\\d+)", streamPage)
	web.Get(url+"stream/", streamIndex)
	web.Get(url+"blog/rss", rss)
	web.Get(url+"blog/atom", atom)

	RssHref = url + "blog/rss"
	AtomHref = url + "blog/atom"
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
func Flatpage(ctx *web.Context, url string) string {
	p := GetPage(url)
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

func Index(s string) string {
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

func blogIndex(ctx *web.Context) string {
	return blogPage(ctx, "1")
}

func blogPage(ctx *web.Context, page string) string {
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

func atom(ctx *web.Context) string {
	feed := _createFeed()
	ctx.Header().Set("Content-Type", "application/xml")
	if feed == nil {
		return "<!-- error -->"
	}
	text, err := feed.ToAtom()
	if err != nil {
		fmt.Println(err)
		return "<!-- error -->"
	}
	return text
}

func rss(ctx *web.Context) string {
	feed := _createFeed()
	ctx.Header().Set("Content-Type", "application/xml")
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

func blogDetail(ctx *web.Context, slug string) string {
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

func streamIndex(ctx *web.Context) string {
	return streamPage(ctx, "1")
}

func streamPage(ctx *web.Context, page string) string {
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

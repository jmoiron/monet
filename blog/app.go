package blog

import (
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"path"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-sprout/sprout"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/monarch"
	"github.com/jmoiron/monet/mtr"
)

//go:embed blog/*
var blogTemplates embed.FS

const defaultPageSize = 15

type App struct {
	db db.DB

	BaseURL     string
	FeedRSSURL  string
	FeedAtomURL string

	PageSize int
}

// NewApp instantiates a new blog app.
func NewApp(db db.DB) *App {
	return &App{db: db, PageSize: defaultPageSize}
}

func (a *App) WithBaseURL(url string) *App {
	a.BaseURL = url
	return a
}

func (a *App) Name() string { return "blog" }

// Attach the blog to r at base.
func (a *App) Bind(r chi.Router) {
	r.Route(a.BaseURL, func(r chi.Router) {
		// support old /blog/slug/ style slash urls
		r.Use(middleware.StripSlashes)
		r.Get("/rss", a.rss)
		r.Get("/atom", a.atom)
		r.Get("/page/{page:[0-9]+}", a.list)
		r.Get("/{slug:[^/]+}", a.detail)
		r.Get("/", a.index)
	})

	a.FeedRSSURL = path.Join(a.BaseURL, "rss")
	a.FeedAtomURL = path.Join(a.BaseURL, "atom")
}

func (a *App) Register(reg *mtr.Registry) {
	// add blog related functions to template context
	reg.Handler.AddRegistry(
		mtr.NewSproutRegistry("blog", sprout.FunctionMap{
			"safe": func(s string) template.HTML {
				return template.HTML(s)
			},
			"naturalTime": func(t time.Time) string {
				return app.FmtTimestamp(t.Unix())
			},
		}),
	)

	reg.AddAllFS(blogTemplates)
}

// Migrate the blog backend.
func (a *App) Migrate() error {
	manager, err := monarch.NewManager(a.db)
	if err != nil {
		return nil
	}

	for _, m := range []monarch.Set{postMigrations, postTagMigrations} {
		if err := manager.Upgrade(m); err != nil {
			return fmt.Errorf("error running %s migration: %w", m.Name, err)
		}
	}

	return nil
}

// Return an Admin object that can render admin homepage panels
// and register all of the administrative pages.
func (a *App) GetAdmin() (app.Admin, error) {
	return NewBlogAdmin(a.db), nil
}

func (a *App) rss(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("content-type", "application/xml")

	/*
		feed := newFeed()
		if feed == nil {
			xml500(w, "empty")
			return
		}
		text, err := feed.ToRss()
		if err != nil {
			fmt.Println(err)
			xml500(w, err.Error())
			return
		}

		w.Write([]byte(text))
	*/
}

func (a *App) atom(w http.ResponseWriter, req *http.Request) {

}

func (a *App) detail(w http.ResponseWriter, req *http.Request) {
	slog.Debug("blog detail", "slug", chi.URLParam(req, "slug"))
	p, err := NewPostService(a.db).GetSlug(chi.URLParam(req, "slug"))
	if err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	reg := mtr.RegistryFromContext(req.Context())
	reg.RenderWithBase(w, "base", "blog/post_detail.html", mtr.Ctx{
		"post": p,
	})
}

func (a *App) list(w http.ResponseWriter, req *http.Request) {
	serv := NewPostService(a.db)

	var count int
	if err := a.db.Get(&count, "SELECT count(*) FROM post WHERE published > 0;"); err != nil {
		app.Http500("getting count", w, err)
		return
	}

	pageNum := app.GetIntParam(req, "page", 1)
	slog.Debug("loading page", "page", pageNum)

	pageBase := path.Join(a.BaseURL, "page")
	paginator := mtr.NewPaginator(a.PageSize, count).WithLinkFn(mtr.SlashLinkFn(pageBase))
	page := paginator.Page(pageNum)

	// select the posts for the page we're trying to render
	q := fmt.Sprintf(`WHERE published > 0 ORDER BY created_at DESC LIMIT %d OFFSET %d`, a.PageSize, page.StartOffset)

	posts, err := serv.Select(q)
	if err != nil {
		app.Http500("loading posts", w, err)
		return
	}

	reg := mtr.RegistryFromContext(req.Context())
	err = reg.RenderWithBase(w, "base", "blog/index.html", mtr.Ctx{
		"posts":      posts,
		"pagination": paginator.Render(reg, page),
	})

	if err != nil {
		slog.Error("rendering template", "err", err)
	}

}

func (a *App) index(w http.ResponseWriter, req *http.Request) {
	a.list(w, req)
}

/*
// A Flatpage view.  Attach it via web.Get wherever you want flatpages to be available
func Flatpage(url string) string {
	p := GetPage(url)
	fmt.Printf("Got page %v for url %s\n", p, url)
	if p == nil {
		ctx.Abort(404, "Page not found")
		return ""
	}
	return ""

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

func newFeed() *feeds.Feed {
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
*/

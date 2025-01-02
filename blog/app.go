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
	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/sqlx"
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

func (a *App) search(w http.ResponseWriter, req *http.Request, query string) {

	// make query safe for fts5
	query = db.SafeQuery(query)

	countq := `SELECT count(*) FROM post_fts WHERE published > 0 AND post_fts MATCH ? ORDER BY rank`
	var count int
	if err := a.db.Get(&count, countq, query); err != nil {
		app.Http500("counting results", w, err)
		return
	}

	reg := mtr.RegistryFromContext(req.Context())

	if count == 0 {
		reg.RenderWithBase(w, "base", "blog/index.html", mtr.Ctx{
			"query": query,
		})
		return
	}

	pageNum := app.GetIntParam(req, "page", 1)
	slog.Debug("loading search page", "page", pageNum)

	pageBase := path.Join(a.BaseURL, "page")
	// XXX: a link function that retains our query
	paginator := mtr.NewPaginator(a.PageSize, count).WithLinkFn(mtr.SlashLinkFn(pageBase))
	page := paginator.Page(pageNum)

	// any makes it easier to use these in sqlx.In
	var slugs []any
	searchq := fmt.Sprintf(`select slug from post_fts where published > 0 AND post_fts
		MATCH ? ORDER BY rank LIMIT %d OFFSET %d`, a.PageSize, page.StartOffset)

	if err := a.db.Select(&slugs, searchq, query); err != nil {
		app.Http500("fetching results", w, err)
		return
	}

	q, args, err := sqlx.In(`select * from post where slug in (?)`, slugs)
	if err != nil {
		app.Http500("forming in query", w, err)
		return
	}

	var posts []Post
	if err := a.db.Select(&posts, q, args...); err != nil {
		app.Http500("fetching posts", w, err)
		return
	}
	// XXX: order posts by the order of slugs

	err = reg.RenderWithBase(w, "base", "blog/index.html", mtr.Ctx{
		"query":      query,
		"posts":      posts,
		"pagination": paginator.Render(reg, page),
	})

	if err != nil {
		slog.Error("rendering template", "err", err)
	}
}

func (a *App) list(w http.ResponseWriter, req *http.Request) {
	serv := NewPostService(a.db)
	req.ParseForm()
	if query := req.Form.Get("q"); len(query) > 0 {
		a.search(w, req, query)
		return
	}

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

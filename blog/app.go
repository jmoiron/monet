package blog

import (
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-sprout/sprout"
	"github.com/gorilla/feeds"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/monet/pkg/vfs"
	"github.com/jmoiron/monet/uploads"
	"github.com/jmoiron/sqlx"
)

//go:embed blog/*
var blogTemplates embed.FS

const defaultPageSize = 15

type App struct {
	db  db.DB
	fss vfs.Registry

	BaseURL     string
	FeedRSSURL  string
	FeedAtomURL string

	PageSize int
}

// NewApp instantiates a new blog app.
func NewApp(db db.DB, fss vfs.Registry) *App {
	return &App{db: db, fss: fss, PageSize: defaultPageSize}
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
			"humanizeBytes": func(size int64) string {
				return humanize.Bytes(uint64(size))
			},
			"fileExt": func(filename string) string {
				ext := strings.ToLower(filepath.Ext(filename))
				if ext != "" && ext[0] == '.' {
					return ext[1:] // Remove the leading dot
				}
				return ext
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

	for _, m := range []monarch.Set{postMigrations, postTagMigrations, postFileMigrations} {
		if err := manager.Upgrade(m); err != nil {
			return fmt.Errorf("error running %s migration: %w", m.Name, err)
		}
	}

	return nil
}

// Return an Admin object that can render admin homepage panels
// and register all of the administrative pages.
func (a *App) GetAdmin() (app.Admin, error) {
	return NewBlogAdmin(a.db, a.fss), nil
}

func (a *App) feed() *feeds.Feed {
	now := time.Now()
	feed := &feeds.Feed{
		Title:       "jmoiron.net blog",
		Link:        &feeds.Link{Href: "http://jmoiron.net/blog"},
		Description: "discussion about tech, footie, photos",
		Author:      &feeds.Author{Name: "Jason Moiron", Email: "jmoiron@jmoiron.net"},
		Created:     now,
	}

	svc := NewPostService(a.db)
	posts, err := svc.Select("where published > 0 order by published_at desc limit 20")
	if err != nil {
		slog.Error("error getting posts", "err", err)
		return feed
	}

	for _, post := range posts {
		feed.Add(&feeds.Item{
			Title:       post.Title,
			Link:        &feeds.Link{Href: "http://jmoiron.net/blog/" + post.Slug + "/"},
			Description: post.ContentRendered,
			Created:     post.CreatedAt,
		})
	}

	return feed
}

func (a *App) rss(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/rss+xml")

	feed := a.feed()
	if feed == nil {
		app.Http500("nothing in feed", w, fmt.Errorf("empty feed"))
		return
	}

	rss, err := feed.ToRss()
	if err != nil {
		app.Http500("building feed", w, err)
		return
	}

	w.Write([]byte(rss))
}

func (a *App) atom(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/atom+xml")

	feed := a.feed()
	if feed == nil {
		app.Http500("nothing in feed", w, fmt.Errorf("empty feed"))
		return
	}

	atom, err := feed.ToAtom()
	if err != nil {
		app.Http500("building feed", w, err)
		return
	}

	w.Write([]byte(atom))
}

func (a *App) detail(w http.ResponseWriter, req *http.Request) {
	slog.Debug("blog detail", "slug", chi.URLParam(req, "slug"))
	postService := NewPostService(a.db)
	p, err := postService.GetSlug(chi.URLParam(req, "slug"))
	if err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	// Load attached files for this post
	attachedFiles, err := postService.GetAttachedFiles(p.ID)
	if err != nil {
		slog.Error("Failed to load attached files", "post_id", p.ID, "err", err)
		attachedFiles = []*uploads.Upload{} // Continue with empty list
	}

	// Add file URLs to attached files
	mapper := a.fss.Mapper()
	for _, file := range attachedFiles {
		if urlPath, err := mapper.GetURL(file.FilesystemName, file.Filename); err == nil {
			file.URL = urlPath
		}
	}

	reg := mtr.RegistryFromContext(req.Context())
	reg.RenderWithBase(w, "base", "blog/post_detail.html", mtr.Ctx{
		"title":         p.Title,
		"ogTitle":       p.Title,
		"ogDescription": p.OgDescription,
		"ogImage":       p.OgImage,
		"post":          p,
		"attachedFiles": attachedFiles,
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

package bookmarks

import (
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-sprout/sprout"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/monet/pkg/vfs"
)

//go:embed bookmarks/*
var bookmarkTemplates embed.FS

const defaultPageSize = 20

type App struct {
	db                db.DB
	screenshotService *ScreenshotService
	fss               vfs.Registry

	BaseURL    string
	PageSize   int
	chromePath string
}

func NewApp(db db.DB) *App {
	return &App{db: db, PageSize: defaultPageSize}
}

func (a *App) WithScreenshotService(service *ScreenshotService) *App {
	a.screenshotService = service
	return a
}

func (a *App) WithFSS(fss vfs.Registry) *App {
	a.fss = fss
	return a
}

// WithChromePath sets the browser binary screenshots are taken with (see conf.Config.ChromePath).
func (a *App) WithChromePath(path string) *App {
	a.chromePath = path
	return a
}

func (a *App) WithBaseURL(url string) *App {
	a.BaseURL = url
	return a
}

func (a *App) Name() string { return "bookmarks" }

func (a *App) Bind(r chi.Router) {
	r.Route(a.BaseURL, func(r chi.Router) {
		r.Use(middleware.StripSlashes)
		r.Get("/page/{page:[0-9]+}", a.list)
		r.Get("/{id:[^/]+}", a.detail)
		r.Get("/", a.index)
	})
}

func (a *App) Register(reg *mtr.Registry) {
	reg.Handler.AddRegistry(
		mtr.NewSproutRegistry("bookmarks", sprout.FunctionMap{
			"safe": func(s string) template.HTML {
				return template.HTML(s)
			},
			"naturalTime": func(t time.Time) string {
				return app.FmtTimestamp(t.Unix())
			},
			"screenshotURL": func(screenshotPath string) string {
				if a.fss == nil {
					slog.Debug("screenshotURL: no fss")
					return ""
				}
				if screenshotPath == "" {
					return ""
				}
				// Extract filename from the screenshot path
				filename := path.Base(screenshotPath)
				// Get the URL for the specific screenshot file
				screenshotURL, err := a.fss.Mapper().GetURL("screenshots", filename)
				if err != nil {
					slog.Debug("screenshotURL: GetURL failed", "error", err)
					return ""
				}
				return screenshotURL
			},
		}),
	)

	reg.AddAllFS(bookmarkTemplates)
}

func (a *App) Migrate() error {
	manager, err := monarch.NewManager(a.db)
	if err != nil {
		return err
	}

	if err := manager.Upgrade(bookmarkMigrations); err != nil {
		return fmt.Errorf("error running %s migration: %w", bookmarkMigrations.Name, err)
	}

	return nil
}

func (a *App) GetAdmin() (app.Admin, error) {
	return NewBookmarkAdmin(a.db, a.fss).WithChromePath(a.chromePath), nil
}

func (a *App) detail(w http.ResponseWriter, req *http.Request) {
	id := chi.URLParam(req, "id")
	slog.Debug("bookmark detail", "id", id)

	b, err := NewBookmarkService(a.db).GetByID(id)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	reg := mtr.RegistryFromContext(req.Context())
	reg.RenderWithBase(w, "base", "bookmarks/detail.html", mtr.Ctx{
		"title":    b.Title,
		"bookmark": b,
	})
}

const (
	viewList  = "list"
	viewCards = "cards"

	viewCookie = "bookmarks_view"
)

// listView is how the bookmarks are laid out: "list" or "cards". Following a
// ?view= link picks one and remembers it in a cookie, so that it carries over
// to searches and other pages, which have no way of passing it along.
func (a *App) listView(w http.ResponseWriter, req *http.Request) string {
	w.Header().Add("Vary", "Cookie")
	if v := req.URL.Query().Get("view"); v == viewList || v == viewCards {
		cookiePath := strings.TrimSuffix(a.BaseURL, "/")
		if cookiePath == "" {
			cookiePath = "/"
		}
		http.SetCookie(w, &http.Cookie{
			Name:     viewCookie,
			Value:    v,
			Path:     cookiePath,
			MaxAge:   365 * 24 * 60 * 60,
			SameSite: http.SameSiteLaxMode,
		})
		return v
	}
	if c, err := req.Cookie(viewCookie); err == nil && c.Value == viewCards {
		return viewCards
	}
	return viewList
}

// viewLinks are the addresses of this same page in each view
func viewLinks(req *http.Request) map[string]string {
	links := map[string]string{}
	for _, v := range []string{viewList, viewCards} {
		q := url.Values{"view": {v}}
		if query := req.URL.Query().Get("q"); query != "" {
			q.Set("q", query)
		}
		links[v] = req.URL.Path + "?" + q.Encode()
	}
	return links
}

// pageLinkFn makes the page links of a paginator, which keep a search going
func (a *App) pageLinkFn(query string) mtr.LinkFn {
	base := path.Join(a.BaseURL, "page")
	return func(p int) string {
		link := path.Join(base, strconv.Itoa(p))
		if query != "" {
			link += "?" + url.Values{"q": {query}}.Encode()
		}
		return link
	}
}

// renderList renders the list page in the view the reader chose
func (a *App) renderList(w http.ResponseWriter, req *http.Request, ctx mtr.Ctx) {
	ctx["view"] = a.listView(w, req)
	ctx["viewLinks"] = viewLinks(req)
	reg := mtr.RegistryFromContext(req.Context())
	if err := reg.RenderWithBase(w, "base", "bookmarks/index.html", ctx); err != nil {
		slog.Error("rendering template", "err", err)
	}
}

func (a *App) search(w http.ResponseWriter, req *http.Request, rawQuery string) {
	query := db.SafeQuery(rawQuery)

	serv := NewBookmarkService(a.db)
	count, err := serv.SearchCount(query)
	if err != nil {
		app.Http500("counting results", w, err)
		return
	}

	reg := mtr.RegistryFromContext(req.Context())

	if count == 0 {
		a.renderList(w, req, mtr.Ctx{"query": query})
		return
	}

	pageNum := app.GetIntParam(req, "page", 1)
	slog.Debug("loading search page", "page", pageNum)

	paginator := mtr.NewPaginator(a.PageSize, count).WithLinkFn(a.pageLinkFn(rawQuery))
	page := paginator.Page(pageNum)

	bookmarks, err := serv.Search(query, a.PageSize, page.StartOffset)
	if err != nil {
		app.Http500("fetching search results", w, err)
		return
	}

	a.renderList(w, req, mtr.Ctx{
		"query":      query,
		"bookmarks":  bookmarks,
		"pagination": paginator.Render(reg, page),
	})
}

func (a *App) list(w http.ResponseWriter, req *http.Request) {
	serv := NewBookmarkService(a.db)
	req.ParseForm()
	if query := req.Form.Get("q"); len(query) > 0 {
		a.search(w, req, query)
		return
	}

	var count int
	if err := a.db.Get(&count, "SELECT count(*) FROM bookmark WHERE published > 0;"); err != nil {
		app.Http500("getting count", w, err)
		return
	}

	pageNum := app.GetIntParam(req, "page", 1)
	slog.Debug("loading page", "page", pageNum)

	paginator := mtr.NewPaginator(a.PageSize, count).WithLinkFn(a.pageLinkFn(""))
	page := paginator.Page(pageNum)

	q := fmt.Sprintf(`WHERE published > 0 ORDER BY created_at DESC LIMIT %d OFFSET %d`, a.PageSize, page.StartOffset)

	bookmarks, err := serv.Select(q)
	if err != nil {
		app.Http500("loading bookmarks", w, err)
		return
	}

	reg := mtr.RegistryFromContext(req.Context())
	a.renderList(w, req, mtr.Ctx{
		"bookmarks":  bookmarks,
		"pagination": paginator.Render(reg, page),
	})
}

func (a *App) index(w http.ResponseWriter, req *http.Request) {
	a.list(w, req)
}

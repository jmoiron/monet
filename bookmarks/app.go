package bookmarks

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
	"github.com/jmoiron/monet/pkg/hotswap"
)

//go:embed bookmarks/*
var bookmarkTemplates embed.FS

const defaultPageSize = 20

type App struct {
	db               db.DB
	screenshotService *ScreenshotService
	fss              hotswap.URLMapper

	BaseURL  string
	PageSize int
}

func NewApp(db db.DB) *App {
	return &App{db: db, PageSize: defaultPageSize}
}

func (a *App) WithScreenshotService(service *ScreenshotService) *App {
	a.screenshotService = service
	return a
}

func (a *App) WithFSS(fss hotswap.URLMapper) *App {
	a.fss = fss
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
				slog.Debug("screenshotURL: called", "path", screenshotPath, "fss", a.fss != nil)
				if a.fss == nil {
					slog.Debug("screenshotURL: no fss")
					return ""
				}
				if screenshotPath == "" {
					slog.Debug("screenshotURL: empty path")
					return ""
				}
				// Extract filename from the screenshot path
				filename := path.Base(screenshotPath)
				slog.Debug("screenshotURL: processing", "path", screenshotPath, "filename", filename)
				// Get the URL for the specific screenshot file
				screenshotURL, err := a.fss.GetURL("screenshots", filename)
				if err != nil {
					slog.Debug("screenshotURL: GetURL failed", "error", err)
					return ""
				}
				slog.Debug("screenshotURL: success", "url", screenshotURL)
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
	return NewBookmarkAdmin(a.db, a.fss), nil
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

func (a *App) search(w http.ResponseWriter, req *http.Request, query string) {
	query = db.SafeQuery(query)

	serv := NewBookmarkService(a.db)
	count, err := serv.SearchCount(query)
	if err != nil {
		app.Http500("counting results", w, err)
		return
	}

	reg := mtr.RegistryFromContext(req.Context())

	if count == 0 {
		reg.RenderWithBase(w, "base", "bookmarks/index.html", mtr.Ctx{
			"query": query,
		})
		return
	}

	pageNum := app.GetIntParam(req, "page", 1)
	slog.Debug("loading search page", "page", pageNum)

	pageBase := path.Join(a.BaseURL, "page")
	paginator := mtr.NewPaginator(a.PageSize, count).WithLinkFn(mtr.SlashLinkFn(pageBase))
	page := paginator.Page(pageNum)

	bookmarks, err := serv.Search(query, a.PageSize, page.StartOffset)
	if err != nil {
		app.Http500("fetching search results", w, err)
		return
	}

	err = reg.RenderWithBase(w, "base", "bookmarks/index.html", mtr.Ctx{
		"query":      query,
		"bookmarks":  bookmarks,
		"pagination": paginator.Render(reg, page),
	})

	if err != nil {
		slog.Error("rendering template", "err", err)
	}
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

	pageBase := path.Join(a.BaseURL, "page")
	paginator := mtr.NewPaginator(a.PageSize, count).WithLinkFn(mtr.SlashLinkFn(pageBase))
	page := paginator.Page(pageNum)

	q := fmt.Sprintf(`WHERE published > 0 ORDER BY created_at DESC LIMIT %d OFFSET %d`, a.PageSize, page.StartOffset)

	bookmarks, err := serv.Select(q)
	if err != nil {
		app.Http500("loading bookmarks", w, err)
		return
	}

	reg := mtr.RegistryFromContext(req.Context())
	err = reg.RenderWithBase(w, "base", "bookmarks/index.html", mtr.Ctx{
		"bookmarks":  bookmarks,
		"pagination": paginator.Render(reg, page),
	})

	if err != nil {
		slog.Error("rendering template", "err", err)
	}
}

func (a *App) index(w http.ResponseWriter, req *http.Request) {
	a.list(w, req)
}
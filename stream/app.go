package stream

import (
	"embed"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/sqlx"
)

const defaultPageSize = 25

//go:embed stream/*
var templates embed.FS

type App struct {
	db db.DB

	BaseURL  string
	PageSize int
}

func NewApp(db db.DB) *App {
	return &App{db: db, PageSize: defaultPageSize}
}

func (a *App) WithBaseURL(url string) *App {
	a.BaseURL = url
	return a
}

func (a *App) Name() string { return "stream" }

func (a *App) Register(reg *mtr.Registry) {
	reg.AddAllFS(templates)
}

func (a *App) Migrate() error {
	manager, err := monarch.NewManager(a.db)
	if err != nil {
		return nil
	}
	return manager.Upgrade(eventMigration)

}

// Return an Admin object that can render admin homepage panels
// and register all of the administrative pages.
func (a *App) GetAdmin() (app.Admin, error) {
	return nil, nil
}

func (a *App) Bind(r chi.Router) {
	r.Route(a.BaseURL, func(r chi.Router) {
		r.Get("/", a.index)
		r.Get("/page/{page:[0-9]+}", a.list)
	})
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	a.list(w, r)
}

func (a *App) list(w http.ResponseWriter, r *http.Request) {
	serv := NewEventService(a.db)
	r.ParseForm()
	if query := r.Form.Get("q"); len(query) > 0 {
		a.search(w, r, query)
		return
	}

	var count int
	if err := a.db.Get(&count, "SELECT count(*) FROM event;"); err != nil {
		app.Http500("getting count", w, err)
		return
	}

	pageNum := 1
	strPage := chi.URLParam(r, "page")
	if len(strPage) > 0 {
		pageNum, _ = strconv.Atoi(strPage)
	}
	slog.Debug("loading page", "page", pageNum, "count", count)

	pageBase := path.Join(a.BaseURL, "page")
	paginator := mtr.NewPaginator(a.PageSize, count).WithLinkFn(mtr.SlashLinkFn(pageBase))
	page := paginator.Page(pageNum)

	// select the posts for the page we're trying to render
	q := fmt.Sprintf(`ORDER BY timestamp DESC LIMIT %d`, a.PageSize)
	if page.Number > 1 {
		q = fmt.Sprintf(`ORDER BY timestamp DESC LIMIT %d OFFSET %d`, a.PageSize, page.StartOffset)
	}

	events, err := serv.Select(q)
	if err != nil {
		app.Http500("loading events", w, err)
		return
	}
	slog.Debug("events", "len", len(events))

	reg := mtr.RegistryFromContext(r.Context())
	reg.RenderWithBase(w, "base", "stream/index.html", mtr.Ctx{
		"events":     events,
		"pagination": paginator.Render(reg, page),
	})
}

func (a *App) search(w http.ResponseWriter, r *http.Request, query string) {

	// make query safe for fts5
	query = db.SafeQuery(query)

	countq := `SELECT count(*) FROM event_fts WHERE event_fts MATCH ? ORDER BY rank`
	var count int
	if err := a.db.Get(&count, countq, query); err != nil {
		app.Http500("counting results", w, err)
		return
	}

	reg := mtr.RegistryFromContext(r.Context())

	if count == 0 {
		reg.RenderWithBase(w, "base", "stream/index.html", mtr.Ctx{
			"query": query,
		})
		return
	}

	pageNum := app.GetIntParam(r, "page", 1)
	slog.Debug("loading search page", "page", pageNum)

	pageBase := path.Join(a.BaseURL, "page")
	// XXX: a link function that retains our query
	paginator := mtr.NewPaginator(a.PageSize, count).WithLinkFn(mtr.SlashLinkFn(pageBase))
	page := paginator.Page(pageNum)

	// any makes it easier to use these in sqlx.In
	searchq := fmt.Sprintf(`select id from event_fts where event_fts
		MATCH ? ORDER BY rank LIMIT %d OFFSET %d`, a.PageSize, page.StartOffset)

	var ids []any
	if err := a.db.Select(&ids, searchq, query); err != nil {
		app.Http500("fetching results", w, err)
		return
	}

	q, args, err := sqlx.In(`select * from event where id in (?)`, ids)
	if err != nil {
		app.Http500("forming in query", w, err)
		return
	}

	var events []Event
	if err := a.db.Select(&events, q, args...); err != nil {
		app.Http500("fetching events", w, err)
		return
	}
	// XXX: order posts by the order of slugs

	err = reg.RenderWithBase(w, "base", "stream/index.html", mtr.Ctx{
		"query":      query,
		"events":     events,
		"pagination": paginator.Render(reg, page),
	})

	if err != nil {
		slog.Error("rendering template", "err", err)
	}

}

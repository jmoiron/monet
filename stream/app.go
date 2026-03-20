package stream

import (
	"bytes"
	"embed"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strconv"
	
	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/auth"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/monet/stream/sources"
	"github.com/jmoiron/sqlx"
)

const defaultPageSize = 25

//go:embed stream/* stream/admin/*
var templates embed.FS

type App struct {
	db db.DB

	BaseURL  string
	PageSize int
	modules  *ModuleRegistry
	runner   *Runner
}

func NewApp(db db.DB) *App {
	modules := NewModuleRegistry(
		sources.NewBluesky(),
		sources.NewGitHub(),
		sources.NewTwitterArchive(),
	)

	return &App{
		db:       db,
		PageSize: defaultPageSize,
		modules:  modules,
		runner:   NewRunner(db, modules),
	}
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
	for _, migration := range []monarch.Set{eventMigration, sourceMigration} {
		if err := manager.Upgrade(migration); err != nil {
			return err
		}
	}
	return NewSourceService(a.db).EnsureDefaults(a.modules.List())

}

// Return an Admin object that can render admin homepage panels
// and register all of the administrative pages.
func (a *App) GetAdmin() (app.Admin, error) {
	return NewAdmin(a.db, a.runner, a.modules), nil
}

func (a *App) Bind(r chi.Router) {
	a.runner.Start()
	r.Route(a.BaseURL, func(r chi.Router) {
		r.Get("/", a.index)
		r.Get("/event/{id:[0-9]+}", a.detail)
		r.Get("/page/{page:[0-9]+}", a.list)
	})
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	a.list(w, r)
}

func (a *App) list(w http.ResponseWriter, r *http.Request) {
	serv := NewEventService(a.db)
	r.ParseForm()
	typeFilter := streamTypeFilter(r.Form.Get("t"))
	if query := r.Form.Get("q"); len(query) > 0 {
		a.search(w, r, query, typeFilter)
		return
	}

	var count int
	countQuery := "SELECT count(*) FROM event WHERE hidden=0"
	countArgs := []any{}
	if typeFilter != "" {
		countQuery += " AND type=?"
		countArgs = append(countArgs, typeFilter)
	}
	if err := a.db.Get(&count, countQuery, countArgs...); err != nil {
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
	q := ""
	args := []any{}
	q = `WHERE hidden=0 `
	if typeFilter != "" {
		q += `AND type=? `
		args = append(args, typeFilter)
	}
	q += fmt.Sprintf(`ORDER BY timestamp DESC LIMIT %d OFFSET %d`, a.PageSize, page.StartOffset)

	events, err := serv.Select(q, args...)
	if err != nil {
		app.Http500("loading events", w, err)
		return
	}
	slog.Debug("events", "len", len(events))

	reg := mtr.RegistryFromContext(r.Context())
	reg.RenderWithBase(w, "base", "stream/index.html", mtr.Ctx{
		"query":      "",
		"type":       typeFilter,
		"types":      a.streamFrontendTypes("", typeFilter),
		"events":     events,
		"pagination": paginator.Render(reg, page),
	})
}

func (a *App) search(w http.ResponseWriter, r *http.Request, query string, typeFilter string) {

	// make query safe for fts5
	query = db.SafeQuery(query)

	countq := `SELECT count(*) FROM event_fts JOIN event ON event.id = event_fts.rowid WHERE event_fts MATCH ? AND event.hidden = 0`
	countArgs := []any{query}
	if typeFilter != "" {
		countq += ` AND event.type = ?`
		countArgs = append(countArgs, typeFilter)
	}
	var count int
	if err := a.db.Get(&count, countq, countArgs...); err != nil {
		app.Http500("counting results", w, err)
		return
	}

	reg := mtr.RegistryFromContext(r.Context())

	if count == 0 {
		reg.RenderWithBase(w, "base", "stream/index.html", mtr.Ctx{
			"query": query,
			"type":  typeFilter,
			"types": a.streamFrontendTypes(query, typeFilter),
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
	searchq := `select event.id from event_fts join event on event.id = event_fts.rowid where event_fts MATCH ? and event.hidden = 0`
	searchArgs := []any{query}
	if typeFilter != "" {
		searchq += ` and event.type = ?`
		searchArgs = append(searchArgs, typeFilter)
	}
	searchq += fmt.Sprintf(` ORDER BY rank LIMIT %d OFFSET %d`, a.PageSize, page.StartOffset)

	var ids []any
	if err := a.db.Select(&ids, searchq, searchArgs...); err != nil {
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
		"type":       typeFilter,
		"types":      a.streamFrontendTypes(query, typeFilter),
		"events":     events,
		"pagination": paginator.Render(reg, page),
	})

	if err != nil {
		slog.Error("rendering template", "err", err)
	}

}

func streamTypeFilter(value string) string {
	switch value {
	case "github", "bluesky", "twitter", "bitbucket":
		return value
	default:
		return ""
	}
}

func (a *App) streamFrontendTypes(query, selected string) []map[string]any {
	items := []struct {
		Value string
		Icon  string
		Label string
	}{
		{Value: "", Icon: "fa-solid fa-bars", Label: "All stream sources"},
		{Value: "bluesky", Icon: "fa-brands fa-bluesky", Label: "Bluesky only"},
		{Value: "github", Icon: "fa-brands fa-github", Label: "GitHub only"},
		{Value: "twitter", Icon: "fa-brands fa-twitter", Label: "Twitter only"},
		{Value: "bitbucket", Icon: "fa-brands fa-bitbucket", Label: "Bitbucket only"},
	}

	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		values := url.Values{}
		if query != "" {
			values.Set("q", query)
		}
		if item.Value != "" {
			values.Set("t", item.Value)
		}
		href := a.BaseURL
		if encoded := values.Encode(); encoded != "" {
			href += "?" + encoded
		}
		out = append(out, map[string]any{
			"value":    item.Value,
			"icon":     item.Icon,
			"label":    item.Label,
			"href":     href,
			"selected": item.Value == selected || (item.Value == "" && selected == ""),
		})
	}
	return out
}

func (a *App) detail(w http.ResponseWriter, r *http.Request) {
	id := app.GetIntParam(r, "id", -1)
	if id < 0 {
		app.Http404(w)
		return
	}

	event, err := NewEventService(a.db).GetByID(id)
	if err != nil {
		app.Http404(w)
		return
	}
	if event.Hidden {
		app.Http404(w)
		return
	}

	reg := mtr.RegistryFromContext(r.Context())
	rawEvent := ""
	if sm := auth.SessionFromContext(r.Context()); sm != nil && sm.IsAuthenticated(r) {
		rawEvent = PrettyEventData(event.Data)
	}
	detailTemplate, detailCtx, err := sources.RenderDetail(event.Type, event.Title, event.Url, event.Data, event.SummaryRendered, event.Timestamp)
	if err != nil {
		app.Http500("rendering stream detail content", w, err)
		return
	}
	var detailBuf bytes.Buffer
	if err := reg.Render(&detailBuf, detailTemplate, mtr.Ctx(detailCtx)); err != nil {
		app.Http500("rendering stream detail partial", w, err)
		return
	}
	err = reg.RenderWithBase(w, "base", "stream/detail.html", mtr.Ctx{
		"title":           event.Title,
		"event":           event,
		"show_meta":       event.Type != "bluesky" && event.Type != "github" && event.Type != "twitter",
		"detail_rendered": detailBuf.String(),
		"raw_event":       rawEvent,
	})
	if err != nil {
		slog.Error("rendering stream detail", "err", err)
	}
}

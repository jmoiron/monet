package pages

import (
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/monet/mtr"
)

//go:embed pages/*
var pageTemplates embed.FS

type App struct {
	db db.DB
}

func NewApp(db db.DB) *App {
	return &App{db: db}
}

func (a *App) Name() string { return "pages" }

func (a *App) Register(reg *mtr.Registry) {
	reg.AddAllFS(pageTemplates)
}

func (a *App) Migrate() error {
	mgr, err := monarch.NewManager(a.db)
	if err != nil {
		return err
	}
	return mgr.Upgrade(pageMigrations)
}

func (a *App) GetAdmin() (app.Admin, error) {
	return NewPageAdmin(a.db), nil
}

func (a *App) Bind(r chi.Router) {
	r.Get("/*", a.index)
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	url := strings.TrimLeft(r.URL.Path, "/")
	serv := NewPageService(a.db)

	p, err := serv.GetByURL(url)
	fmt.Println("getting url", url)
	if err != nil {
		app.Http404(w)
		return
	}

	reg := mtr.RegistryFromContext(r.Context())
	err = reg.RenderWithBase(w, "base", "pages/page.html", mtr.Ctx{
		"title": fmt.Sprintf(p.Title),
		"page":  template.HTML(p.ContentRendered),
	})

	if err != nil {
		slog.Error("rendering template", "err", err)
	}

}

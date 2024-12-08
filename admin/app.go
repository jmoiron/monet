package admin

import (
	"embed"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/auth"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/mtr"
)

// the admin app attaches all of the admins to itself
// it relies on the auth app, but apps register their admin
// capabilities without importing this app, so it should be ok

type App struct {
	db db.DB
	sm *auth.SessionManager

	BaseURL string
	Apps    []app.App
	Admins  []app.Admin
}

//go:embed admin/*
var templates embed.FS

func NewApp(db db.DB, sm *auth.SessionManager) *App {
	return &App{db: db, sm: sm}
}

func (a *App) WithBaseURL(url string) *App {
	a.BaseURL = url
	return a
}

func (a *App) Name() string { return "stream" }

func (a *App) Register(reg *mtr.Registry) {
	reg.AddBaseFS("admin-base", "admin/base.html", templates)
	reg.AddPathFS("admin/index.html", templates)
}

func (a *App) Migrate() error { return nil }

func (a *App) Collect(apps ...app.App) {
	a.Apps = apps
	for _, app := range apps {
		ad, err := app.GetAdmin()
		if err != nil {
			slog.Error("error for adminapp", "app", app.Name(), "err", err)
			continue
		}
		if ad == nil {
			slog.Debug("no admin for app", "app", app.Name())
			continue
		}
		a.Admins = append(a.Admins, ad)
	}
}

// GetAdmin is yo dawg?
func (a *App) GetAdmin() (app.Admin, error) {
	return nil, nil
}

func (a *App) Bind(r chi.Router) {
	// presumably we are bound to something like `/admin/`
	r.Route(a.BaseURL, func(r chi.Router) {
		r.Use(a.sm.RequireAuthenticatedRedirect("/admin/"))
		for _, ad := range a.Admins {
			ad.Bind(r)
		}

		r.Get("/", a.index)
	})
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	var panels []string
	for _, ad := range a.Admins {
		ps, err := ad.Panels(r)
		if err != nil {
			slog.Error("rendering panel", "err", err)
			continue
		}
		panels = append(panels, ps...)
	}

	reg := mtr.RegistryFromContext(r.Context())

	err := reg.RenderWithBase(w, "admin-base", "admin/index.html", mtr.Ctx{
		// FIXME: lets put conf in the context
		"debug":  false,
		"panels": panels,
	})

	if err != nil {
		slog.Error("rendering template", "err", err)
	}
}

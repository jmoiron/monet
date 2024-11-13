package auth

import (
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/conf"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/monarch"
	"github.com/jmoiron/monet/mtr"

	_ "github.com/mattn/go-sqlite3"
)

const (
	loginUrl = "/login/"
)

//go:embed auth/*.html
var authTemplates embed.FS

type App struct {
	db       db.DB
	serv     *UserService
	Sessions *SessionManager
}

// NewApp returns a new authz/n web application.
func NewApp(cfg *conf.Config, db db.DB) *App {
	return &App{
		db:       db,
		serv:     NewUserService(db),
		Sessions: NewSessionManager(cfg),
	}
}

func jsonResp(h http.HandlerFunc) http.HandlerFunc {
	return middleware.SetHeader("content-type", "text/json")(h).(http.HandlerFunc)
}

func (a *App) Name() string { return "auth" }

// Bind this app to endpoints in the router.
func (a *App) Bind(r chi.Router) {
	r.Get("/login/", a.login)
	r.Post("/login/", a.login)

	r.Get("/logout/", a.logout)
	r.Get("/login/status.json", jsonResp(a.status))
}

// Migrate runs db migrations for the Auth application.
func (a *App) Migrate() error {
	m, err := monarch.NewManager(a.db)
	if err != nil {
		return err
	}
	return m.Upgrade(userMigration)
}

func (a *App) Register(r *mtr.Registry) {
	r.AddPathFS("auth/login.html", authTemplates)
}

// GetAdmin returns nil
func (a *App) GetAdmin() (app.Admin, error) {
	return nil, nil
}

func (a *App) login(w http.ResponseWriter, req *http.Request) {
	// if we're trying to log in, validate
	session := a.Sessions.Session(req)
	registry := mtr.RegistryFromContext(req.Context())

	var username string

	if req.Method == "POST" {
		req.ParseForm()
		username = req.Form.Get("username")
		password := req.Form.Get("password")
		redirect := req.Form.Get("redirect")

		if ok, _ := a.serv.Validate(username, password); ok {
			session.Values["authenticated"] = true
			session.Values["user"] = username
			session.Save(req, w)
			slog.Info("user authenticated", "username", username)
			// FIXME: should probably redirect to either referer or admin
			if len(redirect) == 0 {
				redirect = "/"
			}
			http.Redirect(w, req, redirect, 302)
		} else {
			slog.Warn("failed login attempt", "username", username)
			session.AddFlash("invalid username or password")
		}
	}

	// this is either a failed login or a new login attempt; either way,
	// show the login screen
	err := registry.RenderWithBase(w, "base", "auth/login.html", mtr.Ctx{
		"title":    "login",
		"username": username,
		"flashes":  session.Flashes(),
	})

	if err != nil {
		slog.Error("error rendering template", "page", "/login/", "error", err)
	}
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	session := a.Sessions.Session(r)
	session.Values["authenticated"] = false
	session.Values["user"] = ""
	session.Save(r, w)
	http.Redirect(w, r, loginUrl, 302)
}

func (a *App) status(w http.ResponseWriter, r *http.Request) {
	session := a.Sessions.Session(r)
	w.Header().Add("content-type", "text/json")

	json.NewEncoder(w).Encode(
		map[string]string{
			"user":          fmt.Sprint(session.Values["user"]),
			"authenticated": fmt.Sprint(session.Values["authenticated"]),
		})
}

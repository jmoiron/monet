package auth

import (
	"embed"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/jmoiron/monet/conf"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/monarch"
)

const (
	sessionJar = "monet-session"
	loginUrl   = "/login"
)

//go:embed auth/*.html
var authTemplates embed.FS

type App struct {
	db    db.DB
	store sessions.Store
	serv  *UserService
}

// NewApp returns a new authz/n web application.
func NewApp(cfg *conf.Config, db db.DB) *App {
	return &App{
		db:    db,
		serv:  NewUserService(db),
		store: sessions.NewCookieStore([]byte(cfg.SessionSecret)),
	}
}

func (a *App) Name() string { return "auth" }

// Bind this app to endpoints in the router.
func (a *App) Bind(r chi.Router) {
	r.Get("/login/", a.login)
	r.Get("/logout/", a.logout)
	r.Post("/login/", a.login)
}

// Migrate runs db migrations for the Auth application.
func (a *App) Migrate() error {
	m, err := monarch.NewManager(a.db)
	if err != nil {
		return err
	}
	return m.Upgrade(userMigration)
}

func (a *App) RequireAuthenticated(w http.ResponseWriter, req *http.Request) bool {
	session, _ := a.store.Get(req, sessionJar)

	// TODO: forward URL to go back to where you wanted to go
	if session.Values["authenticated"] != true {
		http.Redirect(w, req, loginUrl, 302)
		return false
	}
	return true
}

func (a *App) login(w http.ResponseWriter, req *http.Request) {
	// if we're trying to log in, validate
	session, _ := a.store.Get(req, sessionJar)

	if req.Method == "POST" {
		username, password := req.Form.Get("username"), req.Form.Get("password")
		if ok, _ := a.serv.Validate(username, password); ok {
			session.Values["authenticated"] = true
			session.Values["user"] = username
			session.Save(req, w)
			// FIXME: should probably redirect to either referer or admin
			http.Redirect(w, req, "/", 302)
		} else {
			session.AddFlash("invalid username or password")
		}
	}
	// this is either a failed login or a new login attempt; either way,
	// show the login screen

}

func (a *App) logout(w http.ResponseWriter, req *http.Request) {
	session, _ := a.store.Get(req, "monet-session")
	session.Values["authenticated"] = false
	session.Values["user"] = ""
	session.Save(req, w)
	http.Redirect(w, req, loginUrl, 302)
}

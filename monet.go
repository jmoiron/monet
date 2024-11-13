package main

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/jmoiron/monet/admin"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/auth"
	"github.com/jmoiron/monet/blog"
	"github.com/jmoiron/monet/conf"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/monet/pkg/passwd"
	"github.com/jmoiron/monet/stream"
	"github.com/jmoiron/sqlx"
	"github.com/spf13/pflag"

	_ "github.com/mattn/go-sqlite3"
)

const cfgEnvVar = "MONET_CONFIG_PATH"

type options struct {
	ConfigPath string
	Debug      bool
	AddUser    string
	LoadPosts  string
	LoadEvents string
}

var logLevel = new(slog.LevelVar)

//go:embed static/*
var static embed.FS

//go:embed templates
var templates embed.FS

func main() {
	slog.SetDefault(slog.New(
		slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}),
	))

	static, err := fs.Sub(static, "static")
	if err != nil {
		slog.Error("error w/ static embed", "error", err)
		return
	}

	var opts options
	parseOpts(&opts)

	config, err := loadConfig(opts.ConfigPath)
	if err != nil {
		slog.Error("could not load config")
		return
	}

	if config.Debug {
		slog.Info("debug enabled")
		logLevel.Set(slog.LevelDebug)
	}

	db, err := sqlx.Connect("sqlite3", config.DatabaseURI)
	if err != nil {
		slog.Error("could not connect to database", "uri", config.DatabaseURI, "error", err)
		return
	}

	// the authApp is sort of special;  we want its session middleware to be at the top
	// of our stack, so we want to keep a handle on it
	authApp := auth.NewApp(config, db)

	apps := []app.App{authApp}
	collected, err := collect(config, db)
	if err != nil {
		slog.Error("could not collect apps", "error", err)
		return
	}
	apps = append(apps, collected...)

	reg := mtr.NewRegistry()
	reg.AddBaseFS("base", "templates/base.html", templates)

	for _, app := range apps {
		if err := app.Migrate(); err != nil {
			slog.Error("could not apply migration for app", "name", app.Name())
			return
		}
		app.Register(reg)
	}

	if runUtil(&opts, db) {
		return
	}

	adminApp := admin.NewApp(db, authApp.Sessions).WithBaseURL("/admin/")
	adminApp.Register(reg)

	// build all of the templates
	if err := reg.Build(); err != nil {
		slog.Error("could not build templates", "error", err)
		return
	}

	// set up the router

	r := chi.NewRouter()
	r.Use(authApp.Sessions.AddSessionMiddleware)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(mtr.AddRegistryMiddleware(reg))

	for _, app := range apps {
		app.Bind(r)
	}

	adminApp.Collect(apps...)
	r.Route("/admin/", func(r chi.Router) {
		r.Use(authApp.Sessions.RequireAuthenticatedRedirect("/admin/"))
		adminApp.Bind(r)
	})

	if config.Debug && false {
		fs.WalkDir(static, ".", func(path string, d fs.DirEntry, err error) error {
			slog.Debug("static file", "path", path)
			return nil
		})
	}

	r.Handle("/favicon.ico", http.FileServer(http.FS(static)))
	r.Handle("/static/*", http.FileServer(http.FS(static)))

	slog.Info("Running with config", "config", config.String())
	slog.Info("Listening on", "addr", config.ListenAddr)
	http.ListenAndServe(config.ListenAddr, r)
}

func addUser(db db.DB, username string) error {
	p1, err := passwd.GetPassword(fmt.Sprintf("enter password for user \"%s\"", username))
	if err != nil {
		return err
	}

	p2, err := passwd.GetPassword("repeat password:")
	if err != nil {
		return err
	}

	if p1 != p2 {
		return errors.New("Error: passwords do not match")
	}

	if err := auth.NewUserService(db).CreateUser(username, p1); err != nil {
		return err
	}
	return nil
}

func loadEvents(db db.DB, path string) error {
	loader := stream.NewLoader(db)
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	fmt.Printf("Loading events from %s\n", path)
	defer f.Close()
	return loader.Load(f)
}

func loadPosts(db db.DB, path string) error {
	loader := blog.NewLoader(db)
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	fmt.Printf("Loading posts from %s\n", path)
	defer f.Close()
	return loader.Load(f)
}

func loadConfig(path string) (*conf.Config, error) {
	cfg := conf.Default()

	if len(path) > 0 {
		err := cfg.FromPath(path)
		return cfg, err
	}

	return cfg, nil
}

func parseOpts(opts *options) {
	pflag.StringVarP(&opts.ConfigPath, "config", "c", os.Getenv(cfgEnvVar), "path to a json config file")
	pflag.BoolVarP(&opts.Debug, "debug", "d", false, "enable debug mode")
	pflag.StringVar(&opts.AddUser, "add-user", "", "add a user (will be prompted for pw)")
	pflag.StringVar(&opts.LoadPosts, "load-posts", "", "load posts from json")
	pflag.StringVar(&opts.LoadEvents, "load-events", "", "load events from json")
	pflag.Parse()
}

func runUtil(opts *options, db db.DB) bool {
	switch {
	case len(opts.AddUser) > 0:
		if err := addUser(db, opts.AddUser); err != nil {
			fmt.Printf("Error: %s\n", err)
		}
	case len(opts.LoadPosts) > 0:
		if err := loadPosts(db, opts.LoadPosts); err != nil {
			fmt.Printf("Error: %s\n", err)
		}
	case len(opts.LoadEvents) > 0:
		if err := loadEvents(db, opts.LoadEvents); err != nil {
			fmt.Printf("Error: %s\n", err)
		}
	default:
		return false
	}
	return true
}

func collect(cfg *conf.Config, db db.DB) (apps []app.App, err error) {
	apps = append(apps, blog.NewAppURL(db, "/blog/"))
	apps = append(apps, stream.NewAppURL(db, "/stream/"))
	return apps, nil
}

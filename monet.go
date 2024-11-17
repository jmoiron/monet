package main

import (
	"embed"
	"errors"
	"fmt"
	"io"
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
	"github.com/jmoiron/monet/pages"
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
	LoadPages  string
}

var logLevel = new(slog.LevelVar)

//go:embed static/*
var static embed.FS

//go:embed templates
var templates embed.FS

func must(err error, msg string, args ...any) {
	if err != nil {
		args = append(args, "err", err)
		slog.Error(msg, args...)
		os.Exit(-1)
	}
}

func die[T any](v T, err error) func(string, ...any) T {
	return func(msg string, args ...any) T {
		if err != nil {
			args = append(args, "err", err)
			slog.Error(msg, args...)
			os.Exit(-1)
		}
		return v
	}
}

func main() {
	slog.SetDefault(slog.New(
		slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}),
	))

	var opts options
	parseOpts(&opts)

	static := die(fs.Sub(static, "static"))("initializing static fs")
	config := die(loadConfig(opts.ConfigPath))("loading config")

	if config.Debug {
		slog.Info("debug enabled")
		logLevel.Set(slog.LevelDebug)
	}

	dbh := die(sqlx.Connect("sqlite3", config.DatabaseURI))("uri", config.DatabaseURI)

	// the authApp is sort of special;  we want its session middleware to be at the top
	// of our stack, so we want to keep a handle on it
	var (
		authApp   = auth.NewApp(config, dbh)
		adminApp  = admin.NewApp(dbh, authApp.Sessions).WithBaseURL("/admin/")
		blogApp   = blog.NewApp(dbh).WithBaseURL("/blog/")
		streamApp = stream.NewApp(dbh).WithBaseURL("/stream/")
		pagesApp  = pages.NewApp(dbh)
	)

	// pages should be last as it binds to /*
	apps := []app.App{authApp, adminApp, blogApp, streamApp, pagesApp}

	reg := mtr.NewRegistry()
	reg.AddBaseFS("base", "templates/base.html", templates)
	reg.AddPathFS("templates/index.html", templates)

	for _, app := range apps {
		must(app.Migrate(), "could not migrate app", "name", app.Name())
		app.Register(reg)
	}

	adminApp.Collect(apps...)

	if runUtil(&opts, dbh) {
		return
	}

	must(reg.Build(), "could not build templates")

	// set up the router

	r := chi.NewRouter()
	// add sessions, config, & db
	r.Use(authApp.Sessions.AddSessionMiddleware)
	r.Use(conf.Default().AddConfigMiddleware)
	r.Use(db.AddDbMiddleware(dbh))

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(mtr.AddRegistryMiddleware(reg))

	for _, app := range apps {
		app.Bind(r)
	}

	r.Get("/", index)

	/*
		if config.Debug && false {
			fs.WalkDir(static, ".", func(path string, d fs.DirEntry, err error) error {
				slog.Debug("static file", "path", path)
				return nil
			})
		}
	*/

	r.Handle("/favicon.ico", http.FileServer(http.FS(static)))
	r.Handle("/static/*", http.FileServer(http.FS(static)))

	slog.Info("Running with config", "config", config.String())
	slog.Info("Listening on", "addr", config.ListenAddr)
	http.ListenAndServe(config.ListenAddr, r)
}

func index(w http.ResponseWriter, r *http.Request) {
	reg := mtr.RegistryFromContext(r.Context())
	db := db.DbFromContext(r.Context())

	postalService := blog.NewPostService(db)
	streamService := stream.NewEventService(db)

	posts, err := postalService.Select("WHERE published > 0 ORDER BY published_at DESC LIMIT 6")
	if err != nil {
		app.Http500("loading posts", w, err)
		return
	}
	events, err := streamService.Select("ORDER BY timestamp DESC LIMIT 5")
	if err != nil {
		app.Http500("loading events", w, err)
		return
	}

	err = reg.RenderWithBase(w, "base", "templates/index.html", mtr.Ctx{
		"title":  "jmoiron plays the blues",
		"post":   posts[0],
		"posts":  posts[1:],
		"events": events,
	})

	if err != nil {
		slog.Error("rendering index", "err", err)
	}
}

func addUser(dbh db.DB, username string) error {
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

	return auth.NewUserService(dbh).CreateUser(username, p1)
}

type loader interface {
	Load(io.Reader) error
}

func loadPath(l loader, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return l.Load(f)
}

func loadPages(dbh db.DB, path string) error {
	return loadPath(pages.NewLoader(dbh), path)
}

func loadEvents(dbh db.DB, path string) error {
	return loadPath(stream.NewLoader(dbh), path)
}

func loadPosts(dbh db.DB, path string) error {
	return loadPath(blog.NewLoader(dbh), path)
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
	pflag.StringVar(&opts.LoadPages, "load-pages", "", "load pages from json")
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
	case len(opts.LoadPages) > 0:
		if err := loadPages(db, opts.LoadPages); err != nil {
			fmt.Printf("ERror: %s\n", err)
		}
	default:
		return false
	}
	return true
}

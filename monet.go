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
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/jmoiron/monet/admin"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/auth"
	"github.com/jmoiron/monet/blog"
	"github.com/jmoiron/monet/bookmarks"
	"github.com/jmoiron/monet/conf"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/monet/pages"
	"github.com/jmoiron/monet/pkg/hotswap"
	"github.com/jmoiron/monet/pkg/passwd"
	"github.com/jmoiron/monet/stream"
	"github.com/jmoiron/sqlx"
	"github.com/spf13/pflag"

	"github.com/mattn/go-sqlite3"
)

const (
	cfgEnvVar    = "MONET_CONFIG_PATH"
	monetVersion = "0.0.1-std"
	staticPath   = "static/"
)

type options struct {
	ConfigPath string
	Debug      bool
	Version    bool

	AddUser    string
	LoadPosts  string
	LoadEvents string
	LoadPages  string

	ShowMigration bool
	Downgrade     string
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

func try[T any](v T, err error) func(string, ...any) T {
	return func(msg string, args ...any) T {
		if err != nil {
			args = append(args, "err", err)
			slog.Error(msg, args...)
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

	if opts.Version {
		v, _, t := sqlite3.Version()
		fmt.Printf("Monet v%s\n", monetVersion)
		fmt.Printf("Built w/ Sqlite3 version %v (%s)\n", v, strings.Split(t, " ")[0])
		return
	}

	config := die(loadConfig(opts.ConfigPath))("loading config")

	// set up filesystems
	fss := hotswap.NewURLMapper(config.FSS.URLs)
	for name, path := range config.FSS.Paths {
		if err := fss.AddPath(name, path); err != nil {
			fmt.Printf("Error loading paths: %s", err)
			return
		}
	}

	static := die(fs.Sub(static, "static"))("initializing static fs")
	staticAlt := try(fss.Get("static"))("initializing alternative static path")

	if config.Debug {
		slog.Info("debug enabled")
		logLevel.Set(slog.LevelDebug)
	}

	dbh := die(sqlx.Connect("sqlite3", config.DatabaseURI))("uri", config.DatabaseURI)

	if opts.ShowMigration {
		showMigration(dbh)
		return
	}

	if len(opts.Downgrade) > 0 {
		downgradeApp(dbh, opts.Downgrade)
		return
	}

	// the authApp is sort of special;  we want its session middleware to be at the top
	// of our stack, so we want to keep a handle on it
	var (
		authApp      = auth.NewApp(config, dbh)
		adminApp     = admin.NewApp(dbh, authApp.Sessions).WithBaseURL("/admin/")
		blogApp      = blog.NewApp(dbh).WithBaseURL("/blog/")
		bookmarksApp = bookmarks.NewApp(dbh).WithBaseURL("/bookmarks/").WithFSS(fss)
		streamApp    = stream.NewApp(dbh).WithBaseURL("/stream/")
		pagesApp     = pages.NewApp(dbh)
	)

	// pages should be last as it binds to /*
	apps := []app.App{authApp, adminApp, blogApp, bookmarksApp, streamApp, pagesApp}

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

	swp := hotswap.NewSwapper(static)
	swp.Add(staticAlt)
	if config.Debug {
		swp.Swap()
	}

	reg.DefaultCtx["debug"] = config.Debug

	stack := []func(http.Handler) http.Handler{
		middleware.Compress(5),
		// add a modest cache expiry for static files
		middleware.SetHeader("Cache-Control", "max-age=120"),
	}

	if config.Debug {
		// do not cache when in debug mode
		stack[1] = middleware.NoCache
	}

	r.With(stack...).Handle("/favicon.ico", http.FileServer(http.FS(swp)))
	r.With(stack...).Handle("/static/*", http.FileServer(http.FS(swp)))

	// Serve FSS paths when in debug mode
	if config.Debug {
		allPaths := fss.All()
		for name, path := range allPaths {
			fsys, err := fss.Get(name)
			if err != nil {
				slog.Warn("failed to get filesystem for debug serving", "name", name, "error", err)
				continue
			}
			pattern := fmt.Sprintf("/%s/*", strings.Trim(path, "/"))
			slog.Info("serving debug filesystem", "name", name, "path", path, "pattern", pattern)
			r.With(stack...).Handle(pattern, http.StripPrefix(path, http.FileServer(http.FS(fsys))))
		}
	}

	slog.Info("Running with config", "config", config.String())
	slog.Info("Listening on", "addr", config.ListenAddr)
	if err := http.ListenAndServe(config.ListenAddr, r); err != nil {
		slog.Error("error listening", "err", err)
	}
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
		return errors.New("passwords do not match")
	}

	return auth.NewUserService(dbh).CreateUser(username, p1)
}

func showMigration(db db.DB) {
	m, err := monarch.NewManager(db)
	if err != nil {
		slog.Error("initializing monarch manager", "err", err)
		return
	}

	latest, err := m.LatestVersions()
	if err != nil {
		slog.Error("Error fetching versions", "err", err)
		return
	}

	for _, v := range latest {
		fmt.Printf("app=%s version=%d applied-at=%s\n", v.Name, v.Version, v.AppliedAt)
	}
}

func downgradeApp(db db.DB, app string) {
	m, err := monarch.NewManager(db)
	if err != nil {
		slog.Error("initializing monarch manager", "err", err)
		return
	}

	if err := m.Downgrade(app); err != nil {
		slog.Error("error downgrading app", "app", app, "err", err)
	}
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
	pflag.BoolVarP(&opts.Version, "version", "v", false, "show version info")
	pflag.StringVar(&opts.AddUser, "add-user", "", "add a user (will be prompted for pw)")
	pflag.StringVar(&opts.LoadPosts, "load-posts", "", "load posts from json")
	pflag.StringVar(&opts.LoadEvents, "load-events", "", "load events from json")
	pflag.StringVar(&opts.LoadPages, "load-pages", "", "load pages from json")
	pflag.BoolVar(&opts.ShowMigration, "migrations", false, "show migration state for each application")
	pflag.StringVar(&opts.Downgrade, "downgrade", "", "downgrade an app by one migration version")
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

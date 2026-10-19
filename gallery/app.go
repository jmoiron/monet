package gallery

import (
	"embed"
	"fmt"
	"html"
	"html/template"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-sprout/sprout"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/monet/pkg/vfs"
)

//go:embed gallery/*
var galleryTemplates embed.FS

const (
	defaultPageSize = 24

	// the name of the filesystem photos are kept in (FSS.Paths and FSS.URLs in the config)
	FSName = "photos"
)

// App is the photo gallery: photos from my own uploads, from instagram
// exports, and later from google photos, in one model.
type App struct {
	db  db.DB
	fss vfs.Registry

	BaseURL  string
	PageSize int
}

func NewApp(db db.DB, fss vfs.Registry) *App {
	return &App{db: db, fss: fss, BaseURL: "/gallery/", PageSize: defaultPageSize}
}

func (a *App) WithBaseURL(url string) *App {
	a.BaseURL = url
	return a
}

func (a *App) Name() string { return "gallery" }

func (a *App) Migrate() error {
	manager, err := monarch.NewManager(a.db)
	if err != nil {
		return err
	}
	if err := manager.Upgrade(galleryMigrations); err != nil {
		return fmt.Errorf("error running %s migration: %w", galleryMigrations.Name, err)
	}
	return nil
}

// NewPhotoServiceFromFSS is a photo service on the photos filesystem, which must be
// configured (FSS.Paths.photos) for the gallery to have anywhere to keep files.
func NewPhotoServiceFromFSS(d db.DB, fss vfs.Registry) (*PhotoService, error) {
	dir, err := fss.GetPath(FSName)
	if err != nil {
		return nil, fmt.Errorf(`the gallery needs a "%s" path in FSS.Paths of the config: %w`, FSName, err)
	}
	store, err := NewStore(dir)
	if err != nil {
		return nil, err
	}
	return NewPhotoService(d, store), nil
}

func (a *App) service() (*PhotoService, error) { return NewPhotoServiceFromFSS(a.db, a.fss) }

func (a *App) GetAdmin() (app.Admin, error) {
	return NewAdmin(a.db, a.fss), nil
}

func (a *App) Register(reg *mtr.Registry) {
	reg.Handler.AddRegistry(
		mtr.NewSproutRegistry("gallery", sprout.FunctionMap{
			"photoURL":      photoURLFunc(a.fss),
			"photoDate":     func(t time.Time) string { return fmtTime(t, "Jan 2, 2006") },
			"photoDateTime": func(t time.Time) string { return fmtTime(t, "Jan 2, 2006 · 15:04") },
			"humanBytes":    func(n int64) string { return humanize.Bytes(uint64(n)) },
			"mapLink":       mapLink,
			"photoFacts":    photoFacts,
		}),
	)
	reg.AddAllFS(galleryTemplates)
}

func photoURLFunc(fss vfs.Registry) func(string) string {
	return func(rel string) string {
		if rel == "" || fss == nil {
			return ""
		}
		u, err := fss.Mapper().GetURL(FSName, rel)
		if err != nil {
			slog.Debug("photoURL: GetURL failed", "err", err)
			return ""
		}
		return u
	}
}

func fmtTime(t time.Time, layout string) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(layout)
}

// mapLink is a link to where the photo was taken, on a map.
func mapLink(p *Photo) string {
	if !p.HasGPS() {
		return ""
	}
	return fmt.Sprintf("https://www.openstreetmap.org/?mlat=%.6f&mlon=%.6f#map=14/%.6f/%.6f",
		*p.Latitude, *p.Longitude, *p.Latitude, *p.Longitude)
}

// A Fact is one line of what is known about a photo.
type Fact struct {
	Label string
	Value template.HTML // already escaped
}

// photoFacts lists what is known about a photo, leaving out what isn't.
func photoFacts(p *Photo) []Fact {
	var facts []Fact
	add := func(label, value string) {
		if value != "" {
			facts = append(facts, Fact{label, template.HTML(html.EscapeString(value))})
		}
	}
	add("Taken", fmtTime(p.Taken(), "Jan 2, 2006 · 15:04"))
	add("Camera", p.Camera())
	add("Lens", p.LensModel)
	add("Exposure", p.ExposureSummary())
	if res := p.Resolution(); res != "" {
		if p.Bytes > 0 {
			res += " · " + humanize.Bytes(uint64(p.Bytes))
		}
		add("Size", res)
	}
	if p.HasGPS() {
		facts = append(facts, Fact{"Location", template.HTML(fmt.Sprintf(
			`<a href="%s" target="_blank" rel="noopener">%s</a>`,
			html.EscapeString(mapLink(p)), html.EscapeString(fmt.Sprintf("%.5f, %.5f", *p.Latitude, *p.Longitude))))})
	}
	if p.Source == SourceInstagram {
		// the icon says where it was posted
		var parts []string
		if posted := fmtTime(p.Posted(), "Jan 2, 2006"); posted != "" {
			parts = append(parts, html.EscapeString(posted))
		}
		if p.Likes > 0 {
			parts = append(parts, fmt.Sprintf("%d like%s", p.Likes, plural(p.Likes)))
		}
		if p.Comments > 0 {
			parts = append(parts, fmt.Sprintf("%d comment%s", p.Comments, plural(p.Comments)))
		}
		facts = append(facts, Fact{"Posted", template.HTML(
			`<i class="fa-brands fa-instagram" title="Instagram"></i> ` + strings.Join(parts, " · "))})
	}
	return facts
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (a *App) Bind(r chi.Router) {
	r.Route(a.BaseURL, func(r chi.Router) {
		r.Use(middleware.StripSlashes)
		r.Get("/page/{page:[0-9]+}", a.list)
		r.Get("/collections", a.collections)
		r.Get("/collections/{slug:[^/]+}", a.collection)
		r.Get("/{id:[^/]+}", a.detail)
		r.Get("/", a.list)
	})
}

func (a *App) list(w http.ResponseWriter, req *http.Request) {
	svc, err := a.service()
	if err != nil {
		app.Http500("gallery not configured", w, err)
		return
	}
	count, err := svc.Count("WHERE published > 0")
	if err != nil {
		app.Http500("counting photos", w, err)
		return
	}

	paginator := mtr.NewPaginator(a.PageSize, count).WithLinkFn(mtr.SlashLinkFn(path.Join(a.BaseURL, "page")))
	page := paginator.Page(app.GetIntParam(req, "page", 1))

	photos, err := svc.Select(`WHERE published > 0 `+OrderNewest+` LIMIT ? OFFSET ?`, a.PageSize, page.StartOffset)
	if err != nil {
		app.Http500("loading photos", w, err)
		return
	}

	collections, err := NewAlbumService(a.db).Summaries(true)
	if err != nil {
		slog.Error("loading collections", "err", err)
	}
	reg := mtr.RegistryFromContext(req.Context())
	err = reg.RenderWithBase(w, "base", "gallery/index.html", mtr.Ctx{
		"title":          "Photos",
		"photos":         photos,
		"hasCollections": len(collections) > 0,
		"pagination":     paginator.Render(reg, page),
	})
	if err != nil {
		slog.Error("rendering gallery", "err", err)
	}
}

func (a *App) detail(w http.ResponseWriter, req *http.Request) {
	svc, err := a.service()
	if err != nil {
		app.Http500("gallery not configured", w, err)
		return
	}
	p, err := svc.Get(chi.URLParam(req, "id"))
	if err != nil || p.Published == 0 {
		app.Http404(w)
		return
	}

	albums, err := NewAlbumService(a.db).AlbumsOf(p.ID, true)
	if err != nil {
		slog.Error("loading albums of photo", "err", err)
	}
	reg := mtr.RegistryFromContext(req.Context())
	title := p.Title
	if title == "" {
		title = "Photo"
	}
	err = reg.RenderWithBase(w, "base", "gallery/detail.html", mtr.Ctx{
		"title":  title,
		"photo":  p,
		"albums": albums,
	})
	if err != nil {
		slog.Error("rendering photo", "err", err)
	}
}

package gallery

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strconv"
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

	// Debug is development mode, in which the admin may erase imports to run them again,
	// and searches say why their photos matched
	Debug bool

	search *Searcher
	places *Places
}

func NewApp(db db.DB, fss vfs.Registry) *App {
	return &App{db: db, fss: fss, BaseURL: "/gallery/", PageSize: defaultPageSize, search: NewSearcher(db, SearchConfig{}), places: NewPlaces(db, "")}
}

// WithSearch says how photos are searched by their content.
func (a *App) WithSearch(cfg SearchConfig) *App {
	a.search = NewSearcher(a.db, cfg)
	return a
}

// WithPlaces says where the data to name the places of photos is kept; without
// it photos are not placed.
func (a *App) WithPlaces(dir string) *App {
	a.places = NewPlaces(a.db, dir)
	return a
}

// EnsurePlaces has the data of the places there, fetching it if it isn't, and
// every photo placed. It takes a while, so it is for a goroutine.
func (a *App) EnsurePlaces(ctx context.Context) { a.places.Ensure(ctx) }

// WarmSearch starts what searching needs (the model that embeds queries) in the
// background; for a server that is about to listen, not for a command.
func (a *App) WarmSearch() { a.search.Warm() }

func (a *App) WithDebug(debug bool) *App {
	a.Debug = debug
	return a
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

func (a *App) service() (*PhotoService, error) {
	svc, err := NewPhotoServiceFromFSS(a.db, a.fss)
	if svc != nil {
		svc.places = a.places
	}
	return svc, err
}

func (a *App) GetAdmin() (app.Admin, error) {
	adm := NewAdmin(a.db, a.fss)
	adm.debug = a.Debug
	adm.search, adm.places = a.search, a.places
	return adm, nil
}

func (a *App) Register(reg *mtr.Registry) {
	reg.Handler.AddRegistry(
		mtr.NewSproutRegistry("gallery", sprout.FunctionMap{
			"photoURL":      photoURLFunc(a.fss),
			"photoDate":     func(t time.Time) string { return fmtTime(t, "Jan 2, 2006") },
			"photoDateTime": func(t time.Time) string { return fmtTime(t, "Jan 2, 2006 15:04") },
			"humanBytes":    func(n int64) string { return humanize.Bytes(uint64(n)) },
			"photoPosted":   photoPosted,
			"photoFacts":    photoFacts,
			"photoFactCols": photoFactCols,
			"searchMark":    markHTML,
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

// A Fact is one line of what is known about a photo.
type Fact struct {
	Label string
	Value template.HTML // already escaped
}

// unknown is what a fact that isn't known shows: nothing, but the fact keeps
// its place, so that every photo has the same facts in the same places.
const unknown = ""

// photoFacts lists what is known about a photo. Every photo has all of them,
// in the order they're laid out in two columns; what isn't known is blank.
func photoFacts(p *Photo) []Fact {
	var facts []Fact
	add := func(label, value string) {
		if value == "" {
			value = unknown
		}
		facts = append(facts, Fact{label, template.HTML(html.EscapeString(value))})
	}
	size := ""
	if p.Bytes > 0 {
		size = humanize.Bytes(uint64(p.Bytes))
	}
	gps := ""
	if p.HasGPS() {
		gps = fmt.Sprintf("%.5f, %.5f", *p.Latitude, *p.Longitude)
	}
	iso, aperture, exposure, focal := "", "", p.Exposure, ""
	if p.ISO > 0 {
		iso = strconv.Itoa(p.ISO)
	}
	if p.Aperture > 0 {
		aperture = "f/" + trimFloat(p.Aperture)
	}
	if exposure != "" {
		exposure += "s"
	}
	if p.FocalLength > 0 {
		focal = trimFloat(p.FocalLength) + "mm"
	}
	add("Filename", p.Filename())
	add("Timestamp", fmtTime(photoWhen(p), "Jan 2, 2006 15:04"))
	add("Size", size)
	add("Resolution", p.Resolution())
	add("GPS", gps)
	add("Camera", p.Camera())
	add("ISO", iso)
	add("Aperture", aperture)
	add("Exposure", exposure)
	add("Focal len", focal)
	return facts
}

// photoFactCols is photoFacts in the two columns they're shown in: about the
// file on the left, and about the camera on the right.
func photoFactCols(p *Photo) [][]Fact {
	facts := photoFacts(p)
	return [][]Fact{facts[:5], facts[5:]}
}

// photoPosted says where and when a photo was posted, for those that were: the
// icon says where.
func photoPosted(p *Photo) template.HTML {
	if p.Source != SourceInstagram {
		return ""
	}
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
	return template.HTML(`<i class="fa-brands fa-instagram" title="Instagram"></i> Posted ` + strings.Join(parts, " · "))
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
		r.Get("/photos.json", a.manifest)
		r.Get("/map.json", a.mapSpots)
		r.Get("/collections", a.collections)
		r.Get("/collections/{slug:[^/]+}", a.collection)
		r.Get("/{id:[^/]+}", a.detail)
		r.Get("/", a.list)
	})
}

func (a *App) list(w http.ResponseWriter, req *http.Request) {
	a.search.Warm() // a page of photos may be followed by a search of them
	svc, err := a.service()
	if err != nil {
		app.Http500("gallery not configured", w, err)
		return
	}
	q := strings.TrimSpace(req.URL.Query().Get("q"))
	var (
		count  int
		photos []Photo
		extra  mtr.Ctx
		pageFn = mtr.SlashLinkFn(path.Join(a.BaseURL, "page"))
	)
	var found []SearchTile
	if q != "" {
		res, err := a.search.Search(req.Context(), q, SearchOptions{PublishedOnly: true})
		if err != nil {
			app.Http500("searching", w, err)
			return
		}
		if found, err = searchTiles(svc, res.Matches); err != nil {
			app.Http500("loading photos", w, err)
			return
		}
		count = len(found)
		pageFn = func(n int) string {
			return path.Join(a.BaseURL, "page", strconv.Itoa(n)) + "?" + url.Values{"q": {q}}.Encode()
		}
		extra = mtr.Ctx{"q": q}
		if a.Debug {
			// how the photos matched is for developing the search
			why := map[string]Match{}
			for _, t := range found {
				why[t.Photo.ID] = t.Match
			}
			extra["why"], extra["notes"] = why, res.Notes
		}
	} else if count, err = svc.Count("WHERE published > 0"); err != nil {
		app.Http500("counting photos", w, err)
		return
	}

	paginator := mtr.NewPaginator(a.PageSize, count).WithLinkFn(pageFn)
	page := paginator.Page(app.GetIntParam(req, "page", 1))

	if q != "" {
		for _, t := range found[min(page.StartOffset, len(found)):min(page.EndOffset, len(found))] {
			photos = append(photos, t.Photo)
		}
	} else if photos, err = svc.Select(`WHERE published > 0 `+OrderNewest+` LIMIT ? OFFSET ?`, a.PageSize, page.StartOffset); err != nil {
		app.Http500("loading photos", w, err)
		return
	}

	collections, err := NewAlbumService(a.db).Summaries(true)
	if err != nil {
		slog.Error("loading collections", "err", err)
	}
	reg := mtr.RegistryFromContext(req.Context())
	ctx := mtr.Ctx{
		"title":               "Photos",
		"extraContainerClass": "wide",
		"photos":              photos,
		"hasCollections":      len(collections) > 0,
		"pagination":          paginator.Render(reg, page),
	}
	for k, v := range extra {
		ctx[k] = v
	}
	err = reg.RenderWithBase(w, "base", "gallery/index.html", ctx)
	if err != nil {
		slog.Error("rendering gallery", "err", err)
	}
}

// mapSpots answers in json with the published photos that have a place, for
// the map view of the photos page.
func (a *App) mapSpots(w http.ResponseWriter, req *http.Request) {
	svc, err := a.service()
	if err != nil {
		app.Http500("gallery not configured", w, err)
		return
	}
	photos, err := svc.Select(`WHERE published > 0 AND latitude IS NOT NULL AND longitude IS NOT NULL ` + OrderNewest)
	if err != nil {
		app.Http500("loading photos", w, err)
		return
	}
	url := photoURLFunc(a.fss)
	spots := make([]Spot, 0, len(photos))
	for i := range photos {
		p := &photos[i]
		spots = append(spots, Spot{
			ID: p.ID, Lat: *p.Latitude, Lng: *p.Longitude,
			Thumb: url(p.ThumbFile()),
			Title: p.Title,
			Date:  fmtTime(photoWhen(p), "Jan 2, 2006"),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(spots); err != nil {
		slog.Error("encoding map spots", "err", err)
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

	reg := mtr.RegistryFromContext(req.Context())
	title := p.Title
	if title == "" {
		title = "Photo"
	}
	// the overlay on the page that links here wants just the photo
	if req.URL.Query().Get("partial") != "" {
		if err := reg.Render(w, "gallery/detail.html", mtr.Ctx{"photo": p}); err != nil {
			slog.Error("rendering photo", "err", err)
		}
		return
	}
	err = reg.RenderWithBase(w, "base", "gallery/detail.html", mtr.Ctx{
		"title":               title,
		"photo":               p,
		"extraContainerClass": "wide",
		"hideSiteTitle":       true, // so the photo is not half below the fold
	})
	if err != nil {
		slog.Error("rendering photo", "err", err)
	}
}

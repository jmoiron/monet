package gallery

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/monet/pkg/vfs"
)

const (
	panelListSize = 6
	adminPageSize = 24

	// The most a photo, and an instagram export, may be. Monet puts no other
	// limit on request sizes, but a proxy in front of it may (nginx's
	// client_max_body_size is 1MB unless raised).
	MaxPhotoBytes     = 256 << 20
	MaxInstagramBytes = 1 << 30

	// what a request may carry beyond the file: the multipart framing
	framingSlack = 64 << 10
)

type Admin struct {
	db  db.DB
	fss vfs.Registry

	// slot is the one turn at writing to the gallery that there is
	slot chan struct{}

	maxPhoto, maxInstagram int64
}

func NewAdmin(db db.DB, fss vfs.Registry) *Admin {
	return &Admin{db: db, fss: fss, slot: make(chan struct{}, 1), maxPhoto: MaxPhotoBytes, maxInstagram: MaxInstagramBytes}
}

func (a *Admin) Bind(r chi.Router) {
	r.Get("/gallery/", a.list)
	r.Get("/gallery/{page:[0-9]+}", a.list)
	r.Get("/gallery/map/", a.mapPage)
	r.Get("/gallery/map.json", a.mapSpots)
	r.Post("/gallery/upload/", a.uploadPhoto)
	r.Get("/gallery/insta/", a.instagramPage)
	r.Post("/gallery/insta/", a.importInstagram)
	r.Get("/gallery/edit/{id:[^/]+}", a.edit)
	r.Post("/gallery/edit/{id:[^/]+}", a.save)
	r.Get("/gallery/delete/{id:[^/]+}", a.delete)
	a.bindCollections(r)
}

func (a *Admin) service() (*PhotoService, error) { return NewPhotoServiceFromFSS(a.db, a.fss) }

func (a *Admin) Panels(r *http.Request) ([]string, error) {
	svc, err := a.service()
	if err != nil {
		// not set up (yet): no panel, rather than a broken admin index
		slog.Warn("gallery admin panel", "err", err)
		return nil, nil
	}
	photos, err := svc.Select(OrderNewest+` LIMIT ?`, panelListSize)
	if err != nil {
		return nil, err
	}

	var b bytes.Buffer
	reg := mtr.RegistryFromContext(r.Context())
	err = reg.Render(&b, "gallery/admin/photo-panel.html", mtr.Ctx{
		"fullUrl": "gallery/",
		"title":   "Gallery",
		"photos":  photos,
	})
	if err != nil {
		return nil, err
	}
	return []string{b.String()}, nil
}

func (a *Admin) list(w http.ResponseWriter, r *http.Request) {
	svc, err := a.service()
	if err != nil {
		app.Http500("gallery not configured", w, err)
		return
	}
	filter := ParseFilter(r.URL.Query())
	where, args := filter.Where()
	count, err := svc.Count(where, args...)
	if err != nil {
		app.Http500("counting photos", w, err)
		return
	}

	paginator := mtr.NewPaginator(adminPageSize, count).WithLinkFn(func(p int) string {
		return "/admin/gallery/" + strconv.Itoa(p) + filter.Query()
	})
	page := paginator.Page(app.GetIntParam(r, "page", 1))
	photos, err := svc.Select(where+OrderNewest+` LIMIT ? OFFSET ?`, append(args, adminPageSize, page.StartOffset)...)
	if err != nil {
		app.Http500("loading photos", w, err)
		return
	}

	ctx, err := a.filterCtx(r, svc, filter, filterBar{Action: "/admin/gallery/", GPS: true, Count: count})
	if err != nil {
		app.Http500("loading filters", w, err)
		return
	}
	if ctx["newCollection"], err = a.newCollectionForm(r); err != nil {
		app.Http500("rendering the new collection form", w, err)
		return
	}
	ctx["photos"] = photos
	ctx["pagination"] = paginator.Render(mtr.RegistryFromContext(r.Context()), page)

	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.RenderWithBase(w, "admin-base", "gallery/admin/photo-list.html", ctx); err != nil {
		slog.Error("rendering list", "err", err)
	}
}

// filterBar says how the filter bar is drawn on a page.
type filterBar struct {
	Action string // where the form goes
	GPS    bool   // whether to offer the GPS filter
	Count  int    // how many photos match, shown after the button
}

// filterCtx is the context of a page with the filter bar: the bar itself, as
// "filterBar" (all a template does is put that where it goes), and the query
// that carries the filter to the other views of the gallery, as "query".
func (a *Admin) filterCtx(r *http.Request, svc *PhotoService, f Filter, bar filterBar) (mtr.Ctx, error) {
	years, err := svc.Years()
	if err != nil {
		return nil, err
	}
	collections, err := NewAlbumService(a.db).Summaries(false)
	if err != nil {
		return nil, err
	}

	var b bytes.Buffer
	reg := mtr.RegistryFromContext(r.Context())
	err = reg.Render(&b, "gallery/admin/filter-bar.html", mtr.Ctx{
		"filter":      f,
		"years":       years,
		"collections": collections,
		"sources":     []string{SourceInstagram, SourceUpload},
		"action":      bar.Action,
		"gps":         bar.GPS,
		"count":       bar.Count,
	})
	if err != nil {
		return nil, err
	}
	return mtr.Ctx{
		"query":     f.Query(),
		"filterBar": template.HTML(b.String()),
	}, nil
}

// mapPage shows where the photos were taken.
func (a *Admin) mapPage(w http.ResponseWriter, r *http.Request) {
	svc, err := a.service()
	if err != nil {
		app.Http500("gallery not configured", w, err)
		return
	}
	filter := ParseFilter(r.URL.Query())
	// the map shows the photos there is a place for, whatever the filter says of GPS
	mapped := filter
	mapped.GPS = GPSHas
	where, args := mapped.Where()
	count, err := svc.Count(where, args...)
	if err != nil {
		app.Http500("counting photos", w, err)
		return
	}
	ctx, err := a.filterCtx(r, svc, filter, filterBar{Action: "/admin/gallery/map/", Count: count})
	if err != nil {
		app.Http500("loading filters", w, err)
		return
	}
	ctx["title"] = "Photo map"
	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.RenderWithBase(w, "admin-base", "gallery/admin/photo-map.html", ctx); err != nil {
		slog.Error("rendering map", "err", err)
	}
}

// mapSpots answers in json with the photos that have a place, for the map.
func (a *Admin) mapSpots(w http.ResponseWriter, r *http.Request) {
	svc, err := a.service()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonMap{"success": false, "error": err.Error()})
		return
	}
	filter := ParseFilter(r.URL.Query())
	filter.GPS = GPSHas
	where, args := filter.Where()
	photos, err := svc.Select(where+OrderNewest, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonMap{"success": false, "error": err.Error()})
		return
	}
	reg := a.fss
	spots := make([]Spot, 0, len(photos))
	for i := range photos {
		p := &photos[i]
		spots = append(spots, Spot{
			ID: p.ID, Lat: *p.Latitude, Lng: *p.Longitude,
			Thumb: photoURLFunc(reg)(p.ThumbFile()),
			Title: p.Title,
			Date:  fmtTime(photoWhen(p), "Jan 2, 2006"),
		})
	}
	writeJSON(w, http.StatusOK, spots)
}

// photoWhen is the time a photo goes at: taken, else posted.
func photoWhen(p *Photo) time.Time {
	if t := p.Taken(); !t.IsZero() {
		return t
	}
	return p.Posted()
}

// uploadPhoto takes one image file, in the form field "file", and answers in
// json (it is the other half of the drag & drop upload widget, which sends its
// files one at a time). A file that is already in the gallery as an upload
// (the same bytes) isn't added again.
func (a *Admin) uploadPhoto(w http.ResponseWriter, r *http.Request) {
	svc, err := a.service()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonMap{"success": false, "error": err.Error()})
		return
	}

	// the body is the file and a little framing
	r.Body = http.MaxBytesReader(w, r.Body, a.maxPhoto+framingSlack)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeUploadError(w, err, a.maxPhoto)
		return
	}
	defer r.MultipartForm.RemoveAll()

	f, fh, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, jsonMap{"success": false, "error": "no file in the upload"})
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		writeUploadError(w, err, a.maxPhoto)
		return
	}
	if int64(len(data)) > a.maxPhoto {
		writeUploadError(w, errTooLarge, a.maxPhoto)
		return
	}

	// photos are decoded and stored one at a time: decoding is memory hungry,
	// and the database has one writer
	release, err := a.acquire(r.Context())
	if err != nil {
		return
	}
	defer release()

	if existing, err := svc.FindBySHA(SourceUpload, sha256Hex(data)); err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonMap{"success": false, "error": err.Error()})
		return
	} else if existing != nil {
		writeJSON(w, http.StatusOK, jsonMap{"success": true, "duplicate": true, "id": existing.ID,
			"message": "Already in the gallery"})
		return
	}

	p, err := svc.Add(NewPhoto{Source: SourceUpload, Data: data})
	switch {
	case errors.Is(err, ErrNotAnImage):
		slog.Warn("uploading photo", "file", fh.Filename, "err", err)
		writeJSON(w, http.StatusUnprocessableEntity, jsonMap{"success": false,
			"error": "not an image Monet can read (JPEG, PNG, GIF and WebP are)"})
	case err != nil:
		slog.Error("uploading photo", "file", fh.Filename, "err", err)
		writeJSON(w, http.StatusInternalServerError, jsonMap{"success": false, "error": err.Error()})
	default:
		writeJSON(w, http.StatusOK, jsonMap{"success": true, "id": p.ID, "message": "Added"})
	}
}

// instagramPage is where an instagram export is dropped.
func (a *Admin) instagramPage(w http.ResponseWriter, r *http.Request) {
	reg := mtr.RegistryFromContext(r.Context())
	err := reg.RenderWithBase(w, "admin-base", "gallery/admin/photo-insta.html", mtr.Ctx{
		"title":    "Import from Instagram",
		"maxBytes": a.maxInstagram,
	})
	if err != nil {
		slog.Error("rendering instagram import", "err", err)
	}
}

// importInstagram loads an instagram export (the zip, in the form field
// "file") and answers in json with what the import did. The zip is spooled to
// a file first: it is too big to want in memory, and a zip needs to be seekable.
func (a *Admin) importInstagram(w http.ResponseWriter, r *http.Request) {
	svc, err := a.service()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonMap{"success": false, "error": err.Error()})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, a.maxInstagram+framingSlack)
	reader, err := r.MultipartReader()
	if err != nil {
		writeUploadError(w, err, a.maxInstagram)
		return
	}

	tmp, err := os.CreateTemp("", "instagram-export-*.zip")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonMap{"success": false, "error": err.Error()})
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	got := false
	for !got {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeUploadError(w, err, a.maxInstagram)
			return
		}
		if part.FormName() != "file" || part.FileName() == "" {
			continue
		}
		n, err := io.Copy(tmp, part)
		if err == nil && n > a.maxInstagram {
			err = errTooLarge
		}
		if err != nil {
			writeUploadError(w, err, a.maxInstagram)
			return
		}
		got = true
	}
	if !got {
		writeJSON(w, http.StatusBadRequest, jsonMap{"success": false, "error": "no file in the upload"})
		return
	}
	if err := tmp.Close(); err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonMap{"success": false, "error": err.Error()})
		return
	}

	release, err := a.acquire(r.Context())
	if err != nil {
		return
	}
	defer release()

	// finish the import even if the browser goes away partway
	rep, err := NewInstagramImporter(svc).ImportPath(context.WithoutCancel(r.Context()), tmp.Name())
	if err != nil {
		slog.Warn("importing instagram export", "err", err)
		writeJSON(w, http.StatusUnprocessableEntity, jsonMap{"success": false, "error": err.Error()})
		return
	}
	slog.Info("imported instagram export", "report", rep.String())
	writeJSON(w, http.StatusOK, jsonMap{"success": true, "message": rep.String(), "report": rep})
}

var errTooLarge = errors.New("too large")

type jsonMap map[string]any

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeUploadError answers for a request that couldn't be read, saying so
// plainly when it was too big.
func writeUploadError(w http.ResponseWriter, err error, limit int64) {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) || errors.Is(err, errTooLarge) {
		writeJSON(w, http.StatusRequestEntityTooLarge, jsonMap{"success": false,
			"error": fmt.Sprintf("too large: the limit is %s", humanize.Bytes(uint64(limit)))})
		return
	}
	writeJSON(w, http.StatusBadRequest, jsonMap{"success": false, "error": "couldn't read the upload: " + err.Error()})
}

// acquire waits for a turn to write to the gallery. Uploads and imports go one
// at a time: decoding a photo takes memory, and the database has a single
// writer, which several at once would only fight over. It fails if the request
// goes away while it waits.
func (a *Admin) acquire(ctx context.Context) (release func(), err error) {
	select {
	case a.slot <- struct{}{}:
		return func() { <-a.slot }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (a *Admin) edit(w http.ResponseWriter, r *http.Request) {
	svc, err := a.service()
	if err != nil {
		app.Http500("gallery not configured", w, err)
		return
	}
	p, err := svc.Get(chi.URLParam(r, "id"))
	if err != nil {
		app.Http404(w)
		return
	}
	a.showEdit(w, r, svc, p, "")
}

func (a *Admin) showEdit(w http.ResponseWriter, r *http.Request, svc *PhotoService, p *Photo, message string) {
	albums, err := NewAlbumService(a.db).AlbumsOf(p.ID, false)
	if err != nil {
		slog.Error("loading albums of photo", "err", err)
	}
	reg := mtr.RegistryFromContext(r.Context())
	err = reg.RenderWithBase(w, "admin-base", "gallery/admin/photo-edit.html", mtr.Ctx{
		"photo":   p,
		"albums":  albums,
		"message": message,
	})
	if err != nil {
		slog.Error("rendering edit", "err", err)
	}
}

func (a *Admin) save(w http.ResponseWriter, r *http.Request) {
	svc, err := a.service()
	if err != nil {
		app.Http500("gallery not configured", w, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		app.Http500("parsing form", w, err)
		return
	}
	p, err := svc.Get(chi.URLParam(r, "id"))
	if err != nil {
		app.Http404(w)
		return
	}

	p.Title = strings.TrimSpace(r.Form.Get("title"))
	p.Caption = strings.TrimSpace(r.Form.Get("caption"))
	p.Published = 0
	if r.Form.Get("published") == "1" {
		p.Published = 1
	}
	if err := svc.Save(p); err != nil {
		app.Http500("saving photo", w, err)
		return
	}
	a.showEdit(w, r, svc, p, "Saved")
}

func (a *Admin) delete(w http.ResponseWriter, r *http.Request) {
	svc, err := a.service()
	if err != nil {
		app.Http500("gallery not configured", w, err)
		return
	}
	if err := svc.Delete(chi.URLParam(r, "id")); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			app.Http404(w)
			return
		}
		app.Http500("deleting photo", w, err)
		return
	}
	http.Redirect(w, r, "/admin/gallery/", http.StatusFound)
}

package gallery

import (
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"html"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/monet/pkg/classify"
)

// the most an uploaded export of classifications may be
const maxClassifyBytes = 256 << 20

func (a *Admin) bindClassify(r chi.Router) {
	r.Get("/gallery/classify/", a.classifyPage)
	r.Post("/gallery/classify/", a.importClassifications)
	r.Get("/gallery/places/status/", a.placesStatus)
	r.Post("/gallery/places/rescan/", a.placesRescan)
	r.Post("/gallery/places/refresh/", a.placesRefresh)
	r.Get("/gallery/search/", a.searchPage)
}

// classifyPage is where what a classifier saw is imported, and which models
// have classified how many of the photos.
func (a *Admin) classifyPage(w http.ResponseWriter, r *http.Request) {
	svc, err := a.service()
	if err != nil {
		app.Http500("gallery not configured", w, err)
		return
	}
	models, err := NewClassService(a.db).Models()
	if err != nil {
		app.Http500("loading models", w, err)
		return
	}
	total, err := svc.Count("")
	if err != nil {
		app.Http500("counting photos", w, err)
		return
	}
	reg := mtr.RegistryFromContext(r.Context())
	err = reg.RenderWithBase(w, "admin-base", "gallery/admin/classify.html", mtr.Ctx{
		"title":    "Classifications",
		"models":   models,
		"total":    total,
		"maxBytes": int64(maxClassifyBytes),
	})
	if err != nil {
		slog.Error("rendering classify", "err", err)
	}
}

func (a *Admin) placesStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.places.Status())
}

// placesRescan names the places of all the photos again, from the data there is.
func (a *Admin) placesRescan(w http.ResponseWriter, r *http.Request) {
	n, err := a.places.Scan(true)
	if err != nil {
		writeJSON(w, http.StatusConflict, jsonMap{"success": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, jsonMap{"success": true, "message": "Placed " + strconv.Itoa(n) + " photos"})
}

// placesRefresh fetches the data of the places again and names them again, in
// the background: the page follows it through the status.
func (a *Admin) placesRefresh(w http.ResponseWriter, r *http.Request) {
	if !a.places.Enabled() {
		writeJSON(w, http.StatusConflict, jsonMap{"success": false, "error": "no directory for the geonames data"})
		return
	}
	if busy := a.places.Status().Busy; busy != "" {
		writeJSON(w, http.StatusConflict, jsonMap{"success": false, "error": "already " + busy})
		return
	}
	go func() {
		if err := a.places.Refresh(context.Background()); err != nil {
			slog.Error("refreshing the places", "err", err)
		}
	}()
	writeJSON(w, http.StatusOK, jsonMap{"success": true, "message": "Fetching the data"})
}

// importClassifications takes an export as an uploaded file, and answers with
// a message and a ClassifyReport.
func (a *Admin) importClassifications(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxClassifyBytes+framingSlack)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeUploadError(w, err, maxClassifyBytes)
		return
	}
	defer r.MultipartForm.RemoveAll()
	f, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, jsonMap{"success": false, "error": "no file in the upload"})
		return
	}
	defer f.Close()
	var ex classify.Export
	if err := json.NewDecoder(f).Decode(&ex); err != nil {
		writeJSON(w, http.StatusBadRequest, jsonMap{"success": false, "error": "not an export: " + err.Error()})
		return
	}
	if ex.Version != classify.ExportVersion {
		writeJSON(w, http.StatusBadRequest, jsonMap{"success": false, "error": "an export of version " + strconv.Itoa(ex.Version) + ", not " + strconv.Itoa(classify.ExportVersion)})
		return
	}
	rep, err := NewClassService(a.db).Import(ex.Items)
	if err != nil {
		slog.Error("importing classifications", "err", err)
		writeJSON(w, http.StatusInternalServerError, jsonMap{"success": false, "error": err.Error()})
		return
	}
	msg := "Stored " + strconv.Itoa(rep.Stored)
	for _, n := range []struct {
		n    int
		what string
	}{{rep.Unknown, "unknown"}, {rep.Mismatch, "mismatch"}, {rep.Invalid, "invalid"}} {
		if n.n > 0 {
			msg += " · " + strconv.Itoa(n.n) + " " + n.what
		}
	}
	writeJSON(w, http.StatusOK, jsonMap{"success": true, "message": msg, "report": rep})
}

// A SearchTile is a photo found by a search.
type SearchTile struct {
	Photo Photo
	Match Match
}

// searchTiles are the matches as photos, in the order of the matches. Photos
// that are no longer there are left out.
func searchTiles(svc *PhotoService, matches []Match) ([]SearchTile, error) {
	if len(matches) == 0 {
		return nil, nil
	}
	args := make([]any, len(matches))
	marks := make([]string, len(matches))
	for i, m := range matches {
		args[i], marks[i] = m.ID, "?"
	}
	photos, err := svc.Select(`WHERE id IN (`+strings.Join(marks, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]Photo, len(photos))
	for _, p := range photos {
		byID[p.ID] = p
	}
	var tiles []SearchTile
	for _, m := range matches {
		if p, ok := byID[m.ID]; ok {
			tiles = append(tiles, SearchTile{Photo: p, Match: m})
		}
	}
	return tiles, nil
}

// searchPage finds photos by what is in them, all of them, whether or not they
// are published, and shows the scores and the reasons.
func (a *Admin) searchPage(w http.ResponseWriter, r *http.Request) {
	svc, err := a.service()
	if err != nil {
		app.Http500("gallery not configured", w, err)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	ctx := mtr.Ctx{"title": "Search", "q": q}
	if q != "" {
		res, err := a.search.Search(r.Context(), q, SearchOptions{})
		if err != nil {
			app.Http500("searching", w, err)
			return
		}
		tiles, err := searchTiles(svc, res.Matches)
		if err != nil {
			app.Http500("loading photos", w, err)
			return
		}
		ctx["tiles"], ctx["notes"] = tiles, res.Notes
	}
	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.RenderWithBase(w, "admin-base", "gallery/admin/photo-search.html", ctx); err != nil {
		slog.Error("rendering search", "err", err)
	}
}

// markHTML is text from a search's highlights as html: what is in [brackets]
// is what matched.
func markHTML(s string) template.HTML {
	s = html.EscapeString(s)
	s = strings.ReplaceAll(s, "[", "<mark>")
	s = strings.ReplaceAll(s, "]", "</mark>")
	return template.HTML(s)
}

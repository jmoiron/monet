package gallery

import (
	"bytes"
	"database/sql"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/mtr"
)

// Collections are albums as the admin calls them: named groups of photos that
// the public gallery shows as galleries of their own.

func (a *Admin) bindCollections(r chi.Router) {
	r.Get("/gallery/collections/", a.collections)
	r.Post("/gallery/collections/", a.createCollection)
	r.Get("/gallery/collections/delete/{id:[^/]+}", a.deleteCollection)
	r.Get("/gallery/collections/{id:[^/]+}", a.editCollection)
	r.Post("/gallery/collections/{id:[^/]+}", a.saveCollection)
	r.Post("/gallery/collections/{id:[^/]+}/add", a.addToCollection)
	r.Post("/gallery/collections/{id:[^/]+}/remove", a.removeFromCollection)
}

func (a *Admin) collections(w http.ResponseWriter, r *http.Request) {
	collections, err := NewAlbumService(a.db).Summaries(false)
	if err != nil {
		app.Http500("loading collections", w, err)
		return
	}
	newCollection, err := a.newCollectionForm(r)
	if err != nil {
		app.Http500("rendering the new collection form", w, err)
		return
	}
	reg := mtr.RegistryFromContext(r.Context())
	err = reg.RenderWithBase(w, "admin-base", "gallery/admin/collection-list.html", mtr.Ctx{
		"title":         "Collections",
		"collections":   collections,
		"newCollection": newCollection,
	})
	if err != nil {
		slog.Error("rendering collections", "err", err)
	}
}

// newCollectionForm is the dialog for making a collection, the same wherever
// it's wanted: a page puts it in with {{.newCollection}}, and any button of
// the class "new-collection-button" opens it (one with a data-from-selection
// attribute starts the collection with the photos selected on the page).
func (a *Admin) newCollectionForm(r *http.Request) (template.HTML, error) {
	var b bytes.Buffer
	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.Render(&b, "gallery/admin/collection-new.html", mtr.Ctx{}); err != nil {
		return "", err
	}
	return template.HTML(b.String()), nil
}

// parseForm reads a form posted either way a browser does: a plain form is
// urlencoded, and a FormData is multipart.
func parseForm(r *http.Request) error {
	err := r.ParseMultipartForm(1 << 20)
	if errors.Is(err, http.ErrNotMultipart) {
		return nil
	}
	return err
}

// createCollection makes a collection from the dialog's title and description
// and answers in json with where it is. Photos in the form field "ids" start
// it off.
func (a *Admin) createCollection(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(r); err != nil {
		writeJSON(w, http.StatusBadRequest, jsonMap{"success": false, "error": err.Error()})
		return
	}
	albums := NewAlbumService(a.db)
	album, err := albums.Create(r.Form.Get("title"), strings.TrimSpace(r.Form.Get("description")))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, jsonMap{"success": false, "error": err.Error()})
		return
	}
	if ids := r.Form["ids"]; len(ids) > 0 {
		if err := albums.Add(album.ID, ids...); err != nil {
			slog.Error("filling new collection", "err", err)
			writeJSON(w, http.StatusInternalServerError, jsonMap{"success": false, "error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, jsonMap{"success": true, "id": album.ID,
		"url": "/admin/gallery/collections/" + album.ID, "message": "Created " + album.Title})
}

func (a *Admin) editCollection(w http.ResponseWriter, r *http.Request) {
	a.showCollection(w, r, chi.URLParam(r, "id"), "")
}

func (a *Admin) showCollection(w http.ResponseWriter, r *http.Request, id, message string) {
	albums := NewAlbumService(a.db)
	album, err := albums.Get(id)
	if err != nil {
		app.Http404(w)
		return
	}
	photos, err := albums.Photos(album.ID, false)
	if err != nil {
		app.Http500("loading photos", w, err)
		return
	}
	reg := mtr.RegistryFromContext(r.Context())
	err = reg.RenderWithBase(w, "admin-base", "gallery/admin/collection-edit.html", mtr.Ctx{
		"title":      album.Title,
		"collection": album,
		"photos":     photos,
		"message":    message,
	})
	if err != nil {
		slog.Error("rendering collection", "err", err)
	}
}

func (a *Admin) saveCollection(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		app.Http500("parsing form", w, err)
		return
	}
	albums := NewAlbumService(a.db)
	album, err := albums.Get(chi.URLParam(r, "id"))
	if err != nil {
		app.Http404(w)
		return
	}
	title := strings.TrimSpace(r.Form.Get("title"))
	if title == "" {
		a.showCollection(w, r, album.ID, "A collection needs a title")
		return
	}
	album.Title = title
	album.Description = strings.TrimSpace(r.Form.Get("description"))
	album.Published = 0
	if r.Form.Get("published") == "1" {
		album.Published = 1
	}
	if err := albums.Save(album); err != nil {
		app.Http500("saving collection", w, err)
		return
	}
	a.showCollection(w, r, album.ID, "Saved")
}

func (a *Admin) deleteCollection(w http.ResponseWriter, r *http.Request) {
	albums := NewAlbumService(a.db)
	id := chi.URLParam(r, "id")
	if _, err := albums.Get(id); errors.Is(err, sql.ErrNoRows) {
		app.Http404(w)
		return
	}
	if err := albums.Delete(id); err != nil {
		app.Http500("deleting collection", w, err)
		return
	}
	http.Redirect(w, r, "/admin/gallery/collections/", http.StatusFound)
}

// addToCollection and removeFromCollection take the photos as the form field
// "ids", and answer in json.
func (a *Admin) addToCollection(w http.ResponseWriter, r *http.Request) {
	a.changeCollection(w, r, "Added", func(s *AlbumService, album string, ids []string) error {
		return s.Add(album, ids...)
	})
}

func (a *Admin) removeFromCollection(w http.ResponseWriter, r *http.Request) {
	a.changeCollection(w, r, "Removed", func(s *AlbumService, album string, ids []string) error {
		return s.Remove(album, ids...)
	})
}

func (a *Admin) changeCollection(w http.ResponseWriter, r *http.Request, did string,
	change func(*AlbumService, string, []string) error) {
	if err := parseForm(r); err != nil {
		writeJSON(w, http.StatusBadRequest, jsonMap{"success": false, "error": err.Error()})
		return
	}
	albums := NewAlbumService(a.db)
	album, err := albums.Get(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, jsonMap{"success": false, "error": "no such collection"})
		return
	}
	ids := r.Form["ids"]
	if len(ids) == 0 {
		writeJSON(w, http.StatusBadRequest, jsonMap{"success": false, "error": "no photos chosen"})
		return
	}
	if err := change(albums, album.ID, ids); err != nil {
		slog.Error("changing collection", "err", err)
		writeJSON(w, http.StatusInternalServerError, jsonMap{"success": false, "error": err.Error()})
		return
	}
	n := len(ids)
	msg := did + " " + strconv.Itoa(n) + " photo" + plural(n)
	writeJSON(w, http.StatusOK, jsonMap{"success": true, "message": msg + " · " + album.Title})
}

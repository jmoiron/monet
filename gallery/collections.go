package gallery

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/mtr"
)

// collections lists the published collections, the galleries within the gallery.
func (a *App) collections(w http.ResponseWriter, req *http.Request) {
	collections, err := NewAlbumService(a.db).Summaries(true)
	if err != nil {
		app.Http500("loading collections", w, err)
		return
	}
	reg := mtr.RegistryFromContext(req.Context())
	err = reg.RenderWithBase(w, "base", "gallery/collections.html", mtr.Ctx{
		"title":       "Collections",
		"collections": collections,
	})
	if err != nil {
		slog.Error("rendering collections", "err", err)
	}
}

// collection shows the photos of one published collection, in its order.
func (a *App) collection(w http.ResponseWriter, req *http.Request) {
	albums := NewAlbumService(a.db)
	album, err := albums.BySlug(chi.URLParam(req, "slug"))
	if err != nil || album.Published == 0 {
		app.Http404(w)
		return
	}
	photos, err := albums.Photos(album.ID, true)
	if err != nil {
		app.Http500("loading photos", w, err)
		return
	}
	reg := mtr.RegistryFromContext(req.Context())
	err = reg.RenderWithBase(w, "base", "gallery/collection.html", mtr.Ctx{
		"title":      album.Title,
		"collection": album,
		"photos":     photos,
	})
	if err != nil {
		slog.Error("rendering collection", "err", err)
	}
}

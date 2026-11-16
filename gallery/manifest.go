package gallery

import (
	"net/http"
	"strconv"

	"github.com/jmoiron/monet/app"
)

const (
	defaultManifestSize = 500
	maxManifestSize     = 2000
)

// A ManifestPhoto is a published photo as `classify pull` is told of it.
type ManifestPhoto struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"` // of the original, which the photo's classifications are checked against
	URL    string `json:"url"`    // of the image to look at: the web sized one
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// A Manifest is a page of the published photos, in id order. Next is where the
// following page starts (pass it as "after"), and is empty on the last.
type Manifest struct {
	Photos []ManifestPhoto `json:"photos"`
	Next   string          `json:"next,omitempty"`
}

// manifest lists the published photos for a classifier on another machine to
// fetch, so that what it sees in them can be imported here. It is public because
// the photos are.
func (a *App) manifest(w http.ResponseWriter, req *http.Request) {
	svc, err := a.service()
	if err != nil {
		app.Http500("gallery not configured", w, err)
		return
	}
	limit, err := strconv.Atoi(req.URL.Query().Get("limit"))
	if err != nil || limit <= 0 {
		limit = defaultManifestSize
	}
	limit = min(limit, maxManifestSize)

	photos, err := svc.Select(`WHERE published > 0 AND id > ? ORDER BY id LIMIT ?`, req.URL.Query().Get("after"), limit+1)
	if err != nil {
		app.Http500("loading photos", w, err)
		return
	}
	var m Manifest
	if len(photos) > limit {
		photos = photos[:limit]
		m.Next = photos[limit-1].ID
	}
	url := photoURLFunc(a.fss)
	for _, p := range photos {
		m.Photos = append(m.Photos, ManifestPhoto{ID: p.ID, SHA256: p.SHA256, URL: url(p.DisplayFile()), Width: p.Width, Height: p.Height})
	}
	writeJSON(w, http.StatusOK, m)
}

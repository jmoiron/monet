package bookmarks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/monet/pkg/vfs"
)

const (
	panelListSize = 6
	adminPageSize = 20
)

type Admin struct {
	db      db.DB
	fss     vfs.Registry
	BaseURL string
}

func NewBookmarkAdmin(db db.DB, fss vfs.Registry) *Admin {
	return &Admin{db: db, fss: fss}
}

// newBookmarkServiceWithScreenshots creates a BookmarkService with screenshot capabilities
func (a *Admin) newBookmarkServiceWithScreenshots() *BookmarkService {
	serv := NewBookmarkService(a.db)

	// Get screenshots directory path from FSS
	screenshotsPath, err := a.fss.GetPath("screenshots")
	if err != nil {
		// Return service without screenshot support if path is not available
		return serv
	}

	// Create and set screenshot service
	screenshotService := NewScreenshotService(screenshotsPath, "gowitness", true)
	serv.SetScreenshotService(screenshotService)

	return serv
}

func (a *Admin) Bind(r chi.Router) {
	r.Get("/bookmarks/", a.bookmarkList)
	r.Get("/bookmarks/{page:[0-9]+}", a.bookmarkList)
	r.Get("/bookmarks/edit/{id:[^/]+}", a.edit)

	r.Post("/bookmarks/create/", a.create)
	r.Post("/bookmarks/edit/{id:[^/]+}", a.save)
	r.Get("/bookmarks/ss/{id:[^/]+}", a.screenshot)
	r.Get("/bookmarks/metadata/{id:[^/]+}", a.refreshMetadata)
	r.Post("/bookmarks/image/{id:[^/]+}/{kind:og|screenshot}", a.deleteImage)
	r.Get("/bookmarks/delete/{id:[^/]+}", a.delete)
}

func (a *Admin) Panels(r *http.Request) ([]string, error) {
	serv := NewBookmarkService(a.db)
	bookmarks, err := serv.Select(fmt.Sprintf("ORDER BY created_at DESC LIMIT %d;", panelListSize))
	if err != nil {
		return nil, err
	}

	var panels []string

	reg := mtr.RegistryFromContext(r.Context())

	var b bytes.Buffer
	err = reg.Render(&b, "bookmarks/admin/bookmark-panel.html", mtr.Ctx{
		"fullUrl":   "bookmarks/",
		"title":     "Bookmarks",
		"renderAdd": true,
		"bookmarks": bookmarks,
	})
	if err != nil {
		return nil, err
	}
	panels = append(panels, b.String())

	return panels, nil
}

func (a *Admin) bookmarkList(w http.ResponseWriter, r *http.Request) {
	var count int
	if err := a.db.Get(&count, "SELECT count(*) FROM bookmark;"); err != nil {
		app.Http500("getting count", w, err)
		return
	}

	paginator := mtr.NewPaginator(adminPageSize, count)
	page := paginator.Page(app.GetIntParam(r, "page", 1))

	q := fmt.Sprintf(`ORDER BY created_at DESC LIMIT %d OFFSET %d`, adminPageSize, page.StartOffset)

	serv := NewBookmarkService(a.db)
	bookmarks, err := serv.Select(q)
	if err != nil {
		slog.Error("looking up bookmark", "error", err)
		app.Http404(w)
		return
	}

	reg := mtr.RegistryFromContext(r.Context())
	err = reg.RenderWithBase(w, "admin-base", "bookmarks/admin/bookmark-list.html", mtr.Ctx{
		"bookmarks":  bookmarks,
		"pagination": paginator.Render(reg, page),
	})

	if err != nil {
		slog.Error("rendering list", "err", err)
	}
}

func (a *Admin) edit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	serv := NewBookmarkService(a.db)

	b, err := serv.GetByID(id)
	if err != nil {
		app.Http500("getting by id", w, err)
		return
	}
	a.showEdit(w, r, b)
}

func (a *Admin) showEdit(w http.ResponseWriter, r *http.Request, b *Bookmark) {
	reg := mtr.RegistryFromContext(r.Context())
	err := reg.RenderWithBase(w, "admin-base", "bookmarks/admin/bookmark-edit.html", mtr.Ctx{
		"bookmark": b,
	})
	if err != nil {
		slog.Error("rendering edit", "err", err)
	}
}

func (a *Admin) save(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		app.Http500("parsing form", w, err)
		return
	}

	id := r.Form.Get("id")
	serv := a.newBookmarkServiceWithScreenshots()
	b, err := serv.GetByID(id)
	if err != nil {
		app.Http500("fetching bookmark", w, err)
		return
	}

	b.applyForm(r.Form)

	if err := serv.Save(b); err != nil {
		app.Http500("saving bookmark", w, err)
		return
	}

	a.showEdit(w, r, b)
}

func (a *Admin) create(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		app.Http500("parsing form", w, err)
		return
	}

	url := strings.TrimSpace(r.Form.Get("url"))
	if url == "" {
		app.Http500("URL is required", w, fmt.Errorf("empty URL"))
		return
	}

	serv := a.newBookmarkServiceWithScreenshots()

	// Check if URL already exists
	existing, err := serv.GetByURL(url)
	if err == nil {
		// URL exists, redirect to edit page
		editUrl := fmt.Sprintf("/admin/bookmarks/edit/%s", existing.ID)
		http.Redirect(w, r, editUrl, http.StatusFound)
		return
	}

	// Create new bookmark
	var b Bookmark
	b.URL = url
	b.Title = r.Form.Get("title")

	a.loadCard(r.Context(), &b, false)

	err = serv.Save(&b)
	if err != nil {
		app.Http500("saving bookmark", w, err)
		return
	}

	// Redirect to edit page
	editUrl := fmt.Sprintf("/admin/bookmarks/edit/%s", b.ID)
	http.Redirect(w, r, editUrl, http.StatusFound)
}

// loadCard loads the bookmark's card (see LoadCard). Failures are logged and
// returned; creating a bookmark carries on without a card.
func (a *Admin) loadCard(ctx context.Context, b *Bookmark, overwrite bool) ([]string, error) {
	dir, err := a.fss.GetPath("screenshots")
	if err != nil {
		return nil, fmt.Errorf("no screenshots path for card image: %w", err)
	}
	updated, err := LoadCard(ctx, newBrowserClient(), b, dir, overwrite)
	if err != nil {
		slog.Warn("loading bookmark card", "url", b.URL, "error", err)
		return nil, err
	}
	return updated, nil
}

type MetadataResponse struct {
	Success bool     `json:"success"`
	Updated []string `json:"updated"`
	Error   string   `json:"error,omitempty"`
}

// refreshMetadata reloads the bookmark's card from its site, replacing the
// stored card with the site's current metadata.
func (a *Admin) refreshMetadata(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fail := func(code int, msg string) {
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(MetadataResponse{Error: msg})
	}

	serv := NewBookmarkService(a.db)
	b, err := serv.GetByID(chi.URLParam(r, "id"))
	if err != nil {
		fail(http.StatusNotFound, "bookmark not found")
		return
	}

	updated, err := a.loadCard(r.Context(), b, true)
	if err != nil {
		fail(http.StatusBadGateway, err.Error())
		return
	}
	if err := serv.Save(b); err != nil {
		slog.Error("saving reloaded bookmark card", "id", b.ID, "error", err)
		fail(http.StatusInternalServerError, "saving bookmark: "+err.Error())
		return
	}
	slog.Info("reloaded bookmark card", "id", b.ID, "updated", updated)
	json.NewEncoder(w).Encode(MetadataResponse{Success: true, Updated: updated})
}

type ImageDeleteResponse struct {
	Success bool   `json:"success"`
	Warning string `json:"warning,omitempty"`
	Error   string `json:"error,omitempty"`
}

// deleteImage removes one of the bookmark's images, both from the record and
// from disk. The record is saved first: if removing a file then fails, the worst
// case is an orphaned file, rather than a record pointing at an image that's gone.
func (a *Admin) deleteImage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fail := func(code int, msg string) {
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(ImageDeleteResponse{Error: msg})
	}

	serv := NewBookmarkService(a.db)
	b, err := serv.GetByID(chi.URLParam(r, "id"))
	if err != nil {
		fail(http.StatusNotFound, "bookmark not found")
		return
	}
	dir, err := a.fss.GetPath("screenshots")
	if err != nil {
		fail(http.StatusInternalServerError, "screenshots directory not configured")
		return
	}

	kind := chi.URLParam(r, "kind")
	files, err := b.ClearImage(kind)
	if err != nil {
		fail(http.StatusBadRequest, err.Error())
		return
	}
	if err := serv.Save(b); err != nil {
		slog.Error("saving bookmark after clearing image", "id", b.ID, "kind", kind, "error", err)
		fail(http.StatusInternalServerError, "saving bookmark: "+err.Error())
		return
	}

	resp := ImageDeleteResponse{Success: true}
	if err := RemoveFiles(dir, files); err != nil {
		slog.Warn("removing image files", "id", b.ID, "kind", kind, "files", files, "error", err)
		resp.Warning = "removed from the bookmark, but not every file could be deleted: " + err.Error()
	}
	slog.Info("deleted bookmark image", "id", b.ID, "kind", kind, "files", files)
	json.NewEncoder(w).Encode(resp)
}

func (a *Admin) delete(w http.ResponseWriter, r *http.Request) {
	referer := r.Header.Get("Referer")
	if len(referer) == 0 {
		referer = "/admin/"
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		slog.Warn("deleted non-existent bookmark", "id", id)
		http.Redirect(w, r, referer, http.StatusFound)
		return
	}

	slog.Info("deleting bookmark", "id", id)
	_, err := a.db.Exec("DELETE FROM bookmark WHERE id=?", id)
	if err != nil {
		app.Http500("deleting bookmark", w, err)
		return
	}
	http.Redirect(w, r, referer, http.StatusFound)
}

type ScreenshotResponse struct {
	Success     bool   `json:"success"`
	Filename    string `json:"filename,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Error       string `json:"error,omitempty"`
}

func (a *Admin) screenshot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	id := chi.URLParam(r, "id")
	if id == "" {
		response := ScreenshotResponse{
			Success: false,
			Error:   "bookmark ID is required",
		}
		json.NewEncoder(w).Encode(response)
		return
	}

	// Get the bookmark
	serv := NewBookmarkService(a.db)
	bookmark, err := serv.GetByID(id)
	if err != nil {
		response := ScreenshotResponse{
			Success: false,
			Error:   "bookmark not found",
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Get screenshots directory path from FSS
	screenshotsPath, err := a.fss.GetPath("screenshots")
	if err != nil {
		response := ScreenshotResponse{
			Success: false,
			Error:   "screenshots directory not configured",
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Create screenshot service with FSS-managed directory
	screenshotService := NewScreenshotService(screenshotsPath, "gowitness", true)

	// Take screenshot
	result, err := screenshotService.TakeScreenshot(bookmark.URL, bookmark.ID)
	if err != nil {
		slog.Error("taking screenshot", "id", bookmark.ID, "url", bookmark.URL, "error", err)
		response := ScreenshotResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to take screenshot: %v", err),
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	if result == nil {
		response := ScreenshotResponse{
			Success: false,
			Error:   "screenshot service is disabled",
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Update bookmark with screenshot path, icon path, and title
	bookmark.ScreenshotPath = result.ScreenshotPath
	bookmark.IconPath = result.IconPath
	if bookmark.Title == "" || bookmark.Title == bookmark.URL {
		bookmark.Title = result.Title
	}

	// The page's meta description belongs on the card, not in the bookmark's own
	// description; if the card has none yet, take it from the screenshot's JSON file
	var extractedDescription string
	if bookmark.OGDescription == "" {
		if desc, err := serv.GetDescription(bookmark); err != nil {
			slog.Warn("failed to extract meta description", "error", err)
		} else if desc != "" {
			bookmark.OGDescription = desc
			extractedDescription = desc
			slog.Info("filled card description from meta tag", "bookmark_id", bookmark.ID, "description", desc)
		}
	}

	if err := serv.Save(bookmark); err != nil {
		slog.Error("failed to save bookmark with screenshot info", "error", err)
		// Continue anyway, screenshot was taken successfully
	}

	// Return success response
	response := ScreenshotResponse{
		Success:     true,
		Filename:    result.Filename,
		Title:       result.Title,
		Description: extractedDescription,
	}
	json.NewEncoder(w).Encode(response)
}

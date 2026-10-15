package bookmarks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
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

	b.URL = r.Form.Get("url")
	b.Title = r.Form.Get("title")
	b.Description = r.Form.Get("description")

	formPub, _ := strconv.Atoi(r.Form.Get("published"))
	if b.Published != formPub {
		b.Published = formPub
		if b.Published == 0 {
			var t time.Time
			b.PublishedAt = t
		} else {
			b.PublishedAt = time.Now()
		}
	}

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

	a.populateMetadata(r.Context(), &b)

	err = serv.Save(&b)
	if err != nil {
		app.Http500("saving bookmark", w, err)
		return
	}

	// Redirect to edit page
	editUrl := fmt.Sprintf("/admin/bookmarks/edit/%s", b.ID)
	http.Redirect(w, r, editUrl, http.StatusFound)
}

// populateMetadata loads the bookmarked page and fills in any title,
// description, site name and og:image that the bookmark doesn't already have;
// existing values are never overwritten. It returns the names of the fields it
// set. Failures are logged and otherwise ignored.
func (a *Admin) populateMetadata(ctx context.Context, b *Bookmark) []string {
	var updated []string
	client := newBrowserClient()
	m, err := FetchMetadata(ctx, client, b.URL)
	if err != nil {
		slog.Warn("loading bookmark metadata", "url", b.URL, "error", err)
		return updated
	}
	slog.Debug("bookmark metadata", "url", b.URL, "meta", m)

	if (b.Title == "" || b.Title == b.URL) && m.Title != "" {
		b.Title = m.Title
		updated = append(updated, "title")
	}
	if b.Description == "" && m.Description != "" {
		b.Description = m.Description
		updated = append(updated, "description")
	}
	if b.SiteName == "" && m.SiteName != "" {
		b.SiteName = m.SiteName
		updated = append(updated, "site_name")
	}

	// treat an og image whose file has gone missing as empty
	if b.OGImagePath != "" {
		if _, err := os.Stat(b.OGImagePath); err == nil {
			return updated
		}
	}
	if m.ImageURL == "" {
		return updated
	}
	dir, err := a.fss.GetPath("screenshots")
	if err != nil {
		slog.Warn("no screenshots path for og:image", "error", err)
		return updated
	}
	// the bookmark may have no ID yet (Save treats that as an insert), so name the file independently
	path, err := DownloadImage(ctx, client, m.ImageURL, m.FinalURL, dir, uuid.New().String()+"-og")
	if err != nil {
		slog.Warn("downloading og:image", "url", m.ImageURL, "error", err)
		return updated
	}
	b.OGImagePath = path
	return append(updated, "og_image")
}

type MetadataResponse struct {
	Success bool     `json:"success"`
	Updated []string `json:"updated"`
	Error   string   `json:"error,omitempty"`
}

// refreshMetadata re-runs the metadata fetch for an existing bookmark, filling
// in only the fields that are currently empty.
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

	updated := a.populateMetadata(r.Context(), b)
	if len(updated) > 0 {
		if err := serv.Save(b); err != nil {
			slog.Error("saving refreshed bookmark metadata", "id", b.ID, "error", err)
			fail(http.StatusInternalServerError, "saving bookmark: "+err.Error())
			return
		}
	}
	slog.Info("refreshed bookmark metadata", "id", b.ID, "updated", updated)
	json.NewEncoder(w).Encode(MetadataResponse{Success: true, Updated: updated})
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

	// If description is empty, try to extract it from the screenshot's JSON file
	var extractedDescription string
	if bookmark.Description == "" {
		if desc, err := serv.GetDescription(bookmark); err != nil {
			slog.Warn("failed to extract meta description", "error", err)
		} else if desc != "" {
			bookmark.Description = desc
			extractedDescription = desc
			slog.Info("auto-filled description from meta tag", "bookmark_id", bookmark.ID, "description", desc)
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

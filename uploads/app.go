package uploads

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/monet/pkg/vfs"
)

//go:embed uploads/*
var uploadTemplates embed.FS

// App handles file uploads with database tracking
type App struct {
	db        db.DB
	service   *UploadService
	registry  vfs.Registry
	uploaders map[string]*TrackedUploader
}

// TrackedUploader wraps a VFS uploader with database tracking
type TrackedUploader struct {
	*vfs.Uploader
	filesystemName string
	service        *UploadService
	registry       vfs.Registry
}

// NewApp creates a new uploads app
func NewApp(database db.DB, registry vfs.Registry) *App {
	return &App{
		db:        database,
		service:   NewUploadService(database),
		registry:  registry,
		uploaders: make(map[string]*TrackedUploader),
	}
}

func (a *App) Name() string { return "uploads" }

// CreateUploader creates a tracked uploader for the given filesystem name
func (a *App) CreateUploader(filesystemName string) (*TrackedUploader, error) {
	if uploader, exists := a.uploaders[filesystemName]; exists {
		return uploader, nil
	}

	vfsUploader, err := a.registry.CreateUploader(filesystemName)
	if err != nil {
		return nil, fmt.Errorf("failed to create VFS uploader: %w", err)
	}

	tracked := &TrackedUploader{
		Uploader:       vfsUploader,
		filesystemName: filesystemName,
		service:        a.service,
		registry:       a.registry,
	}

	a.uploaders[filesystemName] = tracked
	return tracked, nil
}

// GetUploader returns an existing tracked uploader for the filesystem name
func (a *App) GetUploader(filesystemName string) (*TrackedUploader, bool) {
	uploader, exists := a.uploaders[filesystemName]
	return uploader, exists
}

// ServeHTTP handles upload requests with database tracking
func (t *TrackedUploader) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		t.handleGet(w, r)
	case http.MethodPost:
		t.handleTrackedPost(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGet serves files (same as VFS uploader)
func (t *TrackedUploader) handleGet(w http.ResponseWriter, r *http.Request) {
	t.Uploader.ServeHTTP(w, r)
}

// handleTrackedPost handles file uploads with database tracking
func (t *TrackedUploader) handleTrackedPost(w http.ResponseWriter, r *http.Request) {
	// Parse the multipart form
	err := r.ParseMultipartForm(64 << 20) // 64MiB max
	if err != nil {
		http.Error(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Failed to get file from form: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Clean the filename
	filename := filepath.Base(header.Filename)
	if filename == "" || filename == "." {
		http.Error(w, "Invalid filename", http.StatusBadRequest)
		return
	}

	// Use the VFS uploader's underlying logic to save the file

	// Get the filesystem to find the base path
	fs, err := t.registry.Get(t.filesystemName)
	if err != nil {
		http.Error(w, "Failed to get filesystem: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// the fs.FS type is read-only, to be writable, it needs to be a PathFS
	var basePath string
	if pfs, ok := fs.(vfs.PathFS); !ok {
		basePath = pfs.Path()
	} else {
		http.Error(w, "Unable to determine filesystem path", http.StatusInternalServerError)
		return
	}

	// Create the full path for the new file
	destPath := filepath.Join(basePath, filename)

	// Create the destination file
	destFile, err := os.Create(destPath)
	if err != nil {
		http.Error(w, "Failed to create file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer destFile.Close()

	// Copy the uploaded file to the destination and track size
	bytesWritten, err := io.Copy(destFile, file)
	if err != nil {
		http.Error(w, "Failed to save file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Create database record with file size
	upload, err := t.service.Create(t.filesystemName, filename, bytesWritten)
	if err != nil {
		slog.Error("Failed to create upload record", "error", err)
		// File was saved but DB record failed - continue anyway
	}

	// Return success with the file URL and database info
	fileURL := t.GetFileURL(filename)
	response := map[string]interface{}{
		"success":  true,
		"filename": filename,
		"url":      fileURL,
	}

	if upload != nil {
		response["id"] = upload.ID
		response["created_at"] = upload.CreatedAt
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// DeleteTracked removes both the file and database record
func (t *TrackedUploader) DeleteTracked(filename string) error {
	// Delete from filesystem first
	err := t.DeleteFile(filename)
	if err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	// Delete from database
	err = t.service.DeleteByFilename(t.filesystemName, filename)
	if err != nil {
		slog.Warn("Failed to delete upload record", "filesystem", t.filesystemName, "filename", filename, "error", err)
		// Don't return error since file was deleted successfully
	}

	return nil
}

// DeleteTrackedByID removes both the file and database record by upload ID
func (t *TrackedUploader) DeleteTrackedByID(id uint64) error {
	// Get the upload record first
	upload, err := t.service.GetByID(id)
	if err != nil {
		return fmt.Errorf("failed to get upload record: %w", err)
	}

	// Delete the file
	err = t.DeleteFile(upload.Filename)
	if err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	// Delete from database
	err = t.service.Delete(id)
	if err != nil {
		slog.Warn("Failed to delete upload record", "id", id, "error", err)
		// Don't return error since file was deleted successfully
	}

	return nil
}

// Attach the uploads app to a router (if needed for admin interface)
func (a *App) Attach(r chi.Router, base string) {
	r.Route(base, func(r chi.Router) {
		r.Get("/list", a.listUploads)
		r.Get("/list/{filesystem}", a.listUploads)
		r.Delete("/{id}", a.deleteUpload)
	})
}

// listUploads returns a JSON list of uploads
func (a *App) listUploads(w http.ResponseWriter, r *http.Request) {
	filesystemName := chi.URLParam(r, "filesystem")

	// Parse query parameters
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 50 // default
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	offset := 0 // default
	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	uploads, err := a.service.List(filesystemName, limit, offset)
	if err != nil {
		http.Error(w, "Failed to list uploads: "+err.Error(), http.StatusInternalServerError)
		return
	}

	count, err := a.service.Count(filesystemName)
	if err != nil {
		http.Error(w, "Failed to count uploads: "+err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"uploads": uploads,
		"total":   count,
		"limit":   limit,
		"offset":  offset,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// deleteUpload deletes an upload by ID
func (a *App) deleteUpload(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid upload ID", http.StatusBadRequest)
		return
	}

	// Get the upload record to find the filesystem
	upload, err := a.service.GetByID(id)
	if err != nil {
		http.Error(w, "Upload not found", http.StatusNotFound)
		return
	}

	// Get or create the uploader for this filesystem
	uploader, err := a.CreateUploader(upload.FilesystemName)
	if err != nil {
		http.Error(w, "Failed to create uploader: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Delete using the tracked uploader
	err = uploader.DeleteTrackedByID(id)
	if err != nil {
		http.Error(w, "Failed to delete upload: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// Migrate runs the database migrations for uploads
func (a *App) Migrate() error {
	manager, err := monarch.NewManager(a.db)
	if err != nil {
		return err
	}

	if err := manager.Upgrade(uploadMigrations); err != nil {
		return fmt.Errorf("error running %s migration: %w", uploadMigrations.Name, err)
	}
	return nil
}

// Register registers templates with the template registry
func (a *App) Register(reg *mtr.Registry) {
	reg.AddPathFS("uploads/admin/upload-panel.html", uploadTemplates)
	reg.AddPathFS("uploads/admin/upload-list.html", uploadTemplates)
}

// Bind sets up the HTTP routes for the uploads app
func (a *App) Bind(r chi.Router) {
	// Bind admin routes
	a.Attach(r, "/uploads")
}

// GetAdmin returns the uploads admin interface
func (a *App) GetAdmin() (app.Admin, error) {
	return NewUploadsAdmin(a.db, a.registry), nil
}

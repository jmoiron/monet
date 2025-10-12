package uploads

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/monet/pkg/vfs"
)

const (
	panelListSize = 5
	adminPageSize = 20
)

var imageFileRegex = regexp.MustCompile(`\.(jpg|jpeg|gif|png)$`)

// Admin handles the uploads admin interface
type Admin struct {
	db       db.DB
	service  *UploadService
	registry vfs.Registry
}

// NewUploadsAdmin creates a new uploads admin
func NewUploadsAdmin(database db.DB, registry vfs.Registry) *Admin {
	return &Admin{
		db:       database,
		service:  NewUploadService(database),
		registry: registry,
	}
}

// Bind sets up the admin routes
func (a *Admin) Bind(r chi.Router) {
	r.Route("/uploads", func(r chi.Router) {
		r.Get("/", a.list)
		r.Get("/fs/{filesystem}", a.listFilesystem)
		r.Get("/delete/{id}", a.deleteUpload)
		r.Post("/rename/{id}", a.renameUpload)
		r.Post("/upload", a.uploadFile)
		r.Post("/upload/{filesystem}", a.uploadFileToFilesystem)
	})
}

// Panels generates the admin panel content
func (a *Admin) Panels(r *http.Request) ([]string, error) {
	uploads, err := a.service.List("", panelListSize, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to get uploads for panel: %w", err)
	}

	// Transform uploads for template
	var uploadItems []map[string]interface{}
	for _, upload := range uploads {
		fileURL, err := a.getFileURL(upload.FilesystemName, upload.Filename)
		if err != nil {
			fileURL = "#" // Fallback if URL generation fails
		}

		uploadItems = append(uploadItems, map[string]interface{}{
			"ID":             upload.ID,
			"FilesystemName": upload.FilesystemName,
			"Filename":       upload.Filename,
			"Size":           upload.Size,
			"SizeHuman":      humanize.Bytes(uint64(upload.Size)),
			"CreatedAt":      upload.CreatedAt,
			"FileURL":        fileURL,
		})
	}

	var b strings.Builder
	reg := mtr.RegistryFromContext(r.Context())
	err = reg.Render(&b, "uploads/admin/upload-panel.html", mtr.Ctx{
		"title":     "Uploads",
		"fullUrl":   "/admin/uploads/",
		"renderAdd": false,
		"uploads":   uploadItems,
	})
	if err != nil {
		return nil, err
	}

	return []string{b.String()}, nil
}

// list shows all uploads in a grid/table layout
func (a *Admin) list(w http.ResponseWriter, r *http.Request) {
	a.renderList(w, r, "", "All Uploads")
}

// listFilesystem shows uploads filtered by filesystem
func (a *Admin) listFilesystem(w http.ResponseWriter, r *http.Request) {
	filesystem := chi.URLParam(r, "filesystem")
	title := fmt.Sprintf("Uploads - %s", filesystem)
	a.renderList(w, r, filesystem, title)
}

// renderList renders the upload list page
func (a *Admin) renderList(w http.ResponseWriter, r *http.Request, filesystem, title string) {
	// Get total count for pagination
	totalCount, err := a.service.Count(filesystem)
	if err != nil {
		http.Error(w, "Failed to count uploads: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Create paginator and get current page
	paginator := mtr.NewPaginator(adminPageSize, totalCount)
	pageNum := app.GetIntParam(r, "page", 1)
	page := paginator.Page(pageNum)

	// Get uploads for this page
	uploads, err := a.service.List(filesystem, adminPageSize, page.StartOffset)
	if err != nil {
		http.Error(w, "Failed to get uploads: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Transform uploads for template with file URLs
	var uploadItems []map[string]interface{}
	for _, upload := range uploads {
		fileURL, err := a.getFileURL(upload.FilesystemName, upload.Filename)
		if err != nil {
			fileURL = "#" // Fallback if URL generation fails
		}

		uploadItems = append(uploadItems, map[string]interface{}{
			"ID":             upload.ID,
			"FilesystemName": upload.FilesystemName,
			"Filename":       upload.Filename,
			"Size":           upload.Size,
			"SizeHuman":      humanize.Bytes(uint64(upload.Size)),
			"CreatedAt":      upload.CreatedAt,
			"FileURL":        fileURL,
			"FilesystemURL":  fmt.Sprintf("/admin/uploads/fs/%s", upload.FilesystemName),
			"IsImage":        imageFileRegex.MatchString(strings.ToLower(upload.Filename)),
		})
	}

	reg := mtr.RegistryFromContext(r.Context())
	err = reg.RenderWithBase(w, "admin-base", "uploads/admin/upload-list.html", mtr.Ctx{
		"title":      title,
		"uploads":    uploadItems,
		"filesystem": filesystem,
		"pagination": paginator.Render(reg, page),
	})

	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// deleteUpload handles delete requests from the admin interface
func (a *Admin) deleteUpload(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid upload ID", http.StatusBadRequest)
		return
	}

	// Get the upload record to find filesystem and filename
	upload, err := a.service.GetByID(id)
	if err != nil {
		http.Error(w, "Upload not found", http.StatusNotFound)
		return
	}

	// Delete the file from filesystem
	_, err = a.getFileURL(upload.FilesystemName, upload.Filename)
	if err == nil {
		// Try to create uploader to delete file properly
		// For now, we'll just delete the database record
		// File deletion could be added later if needed
	}

	// Delete from database
	err = a.service.Delete(id)
	if err != nil {
		http.Error(w, "Failed to delete upload: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Redirect back to the list
	referer := r.Header.Get("Referer")
	if referer != "" && strings.Contains(referer, "/admin/uploads") {
		http.Redirect(w, r, referer, http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/admin/uploads/", http.StatusSeeOther)
	}
}

// uploadFile handles file uploads to the default "uploads" filesystem
func (a *Admin) uploadFile(w http.ResponseWriter, r *http.Request) {
	a.handleFileUpload(w, r, "uploads")
}

// uploadFileToFilesystem handles file uploads to a specific filesystem
func (a *Admin) uploadFileToFilesystem(w http.ResponseWriter, r *http.Request) {
	filesystem := chi.URLParam(r, "filesystem")
	if filesystem == "" {
		filesystem = "uploads"
	}
	a.handleFileUpload(w, r, filesystem)
}

// handleFileUpload processes a file upload using the uploads library
func (a *Admin) handleFileUpload(w http.ResponseWriter, r *http.Request, filesystemName string) {
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

	// Get the filesystem
	fs, err := a.registry.Get(filesystemName)
	if err != nil {
		http.Error(w, "Failed to get filesystem: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get the base path
	var basePath string
	if pfs, ok := fs.(vfs.PathFS); ok {
		basePath = pfs.Path()
	} else {
		http.Error(w, "Filesystem "+filesystemName+" is not writable", http.StatusInternalServerError)
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
	upload, err := a.service.Create(filesystemName, filename, bytesWritten)
	if err != nil {
		http.Error(w, "Failed to create upload record: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get file URL for response
	fileURL, err := a.getFileURL(upload.FilesystemName, upload.Filename)
	if err != nil {
		fileURL = "#" // Fallback
	}

	// Return success with the file info
	response := map[string]interface{}{
		"success":    true,
		"id":         upload.ID,
		"filename":   upload.Filename,
		"size":       upload.Size,
		"created_at": upload.CreatedAt,
		"url":        fileURL,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// renameUpload handles renaming an upload file
func (a *Admin) renameUpload(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid upload ID", http.StatusBadRequest)
		return
	}

	newFilename := strings.TrimSpace(r.FormValue("filename"))
	if newFilename == "" {
		http.Error(w, "Filename cannot be empty", http.StatusBadRequest)
		return
	}

	// Clean the filename
	newFilename = filepath.Base(newFilename)
	if newFilename == "" || newFilename == "." {
		http.Error(w, "Invalid filename", http.StatusBadRequest)
		return
	}

	// Get the upload record
	upload, err := a.service.GetByID(id)
	if err != nil {
		http.Error(w, "Upload not found", http.StatusNotFound)
		return
	}

	// Get the filesystem
	fs, err := a.registry.Get(upload.FilesystemName)
	if err != nil {
		http.Error(w, "Failed to get filesystem: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get the base path
	var basePath string
	if pfs, ok := fs.(vfs.PathFS); ok {
		basePath = pfs.Path()
	} else {
		http.Error(w, "Filesystem "+upload.FilesystemName+" is not writable", http.StatusInternalServerError)
		return
	}

	// Create the old and new file paths
	oldPath := filepath.Join(basePath, upload.Filename)
	newPath := filepath.Join(basePath, newFilename)

	// Check if new filename already exists
	if _, err := os.Stat(newPath); err == nil {
		http.Error(w, "A file with that name already exists", http.StatusConflict)
		return
	}

	// Rename the file on filesystem
	err = os.Rename(oldPath, newPath)
	if err != nil {
		http.Error(w, "Failed to rename file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Update the database record
	err = a.service.UpdateFilename(id, newFilename)
	if err != nil {
		// Try to rename the file back if database update fails
		os.Rename(newPath, oldPath)
		http.Error(w, "Failed to update database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Return success
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"filename": newFilename,
	})
}

// getFileURL generates the public URL for a file
func (a *Admin) getFileURL(filesystemName, filename string) (string, error) {
	if a.registry.Mapper() == nil {
		return "", fmt.Errorf("no URL mapper available")
	}

	return a.registry.Mapper().GetURL(filesystemName, filename)
}

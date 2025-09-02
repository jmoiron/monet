package vfs

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Uploader handles file uploads and serves uploaded files
type Uploader struct {
	fs        fs.FS
	urlPrefix string
	basePath  string // physical path for writing files
}

// NewUploader creates a new uploader with the given filesystem and URL prefix
func NewUploader(filesystem fs.FS, urlPrefix string) (*Uploader, error) {
	var basePath string
	
	// Try to get the base path if this is a PathFS
	if pfs, ok := filesystem.(PathFS); ok {
		basePath = pfs.Path()
	} else {
		return nil, fmt.Errorf("uploader requires a PathFS to write files")
	}
	
	return &Uploader{
		fs:        filesystem,
		urlPrefix: strings.TrimSuffix(urlPrefix, "/"),
		basePath:  basePath,
	}, nil
}

// ServeHTTP implements http.Handler for serving uploaded files (GET requests)
func (u *Uploader) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		u.handleGet(w, r)
	} else if r.Method == http.MethodPost {
		u.handlePost(w, r)
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// GetHandler returns an http.Handler that only serves files (GET requests)
func (u *Uploader) GetHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		u.handleGet(w, r)
	})
}

// PostHandler returns an http.Handler that only accepts uploads (POST requests)
func (u *Uploader) PostHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		u.handlePost(w, r)
	})
}

func (u *Uploader) handleGet(w http.ResponseWriter, r *http.Request) {
	// Strip the URL prefix to get the file path
	path := strings.TrimPrefix(r.URL.Path, u.urlPrefix)
	path = strings.TrimPrefix(path, "/")
	
	if path == "" {
		http.Error(w, "File not specified", http.StatusBadRequest)
		return
	}
	
	// Serve the file using http.FileServer
	fileServer := http.FileServer(http.FS(u.fs))
	
	// Create a new request with the stripped path
	r.URL.Path = "/" + path
	fileServer.ServeHTTP(w, r)
}

func (u *Uploader) handlePost(w http.ResponseWriter, r *http.Request) {
	// Parse the multipart form
	err := r.ParseMultipartForm(32 << 20) // 32MB max
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
	
	// Create the full path for the new file
	destPath := filepath.Join(u.basePath, filename)
	
	// Create the destination file
	destFile, err := os.Create(destPath)
	if err != nil {
		http.Error(w, "Failed to create file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer destFile.Close()
	
	// Copy the uploaded file to the destination
	_, err = io.Copy(destFile, file)
	if err != nil {
		http.Error(w, "Failed to save file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	
	// Return success with the file URL
	fileURL := fmt.Sprintf("%s/%s", u.urlPrefix, filename)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"success": true, "filename": "%s", "url": "%s"}`, filename, fileURL)
}

// GetFileURL returns the URL for a given filename
func (u *Uploader) GetFileURL(filename string) string {
	return fmt.Sprintf("%s/%s", u.urlPrefix, filename)
}

// DeleteFile removes a file from the uploader's filesystem
func (u *Uploader) DeleteFile(filename string) error {
	// Clean the filename to prevent path traversal
	filename = filepath.Base(filename)
	if filename == "" || filename == "." {
		return fmt.Errorf("invalid filename")
	}
	
	filePath := filepath.Join(u.basePath, filename)
	return os.Remove(filePath)
}
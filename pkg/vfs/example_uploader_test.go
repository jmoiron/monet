package vfs_test

import (
	"fmt"
	"net/http"

	"github.com/jmoiron/monet/pkg/vfs"
)

// Example showing how to use the uploader functionality
func ExampleUploader() {
	// Create a URL mapper
	urlMap := map[string]string{
		"uploads": "/files",
		"images":  "/static/img",
	}
	urlMapper := vfs.NewURLMapper(urlMap)

	// Create a registry with the URL mapper
	registry := vfs.NewRegistry(urlMapper)

	// Add a filesystem path for uploads
	err := registry.AddPath("uploads", "/tmp/uploads")
	if err != nil {
		panic(err)
	}

	// Create an uploader for the uploads filesystem
	uploader, err := registry.CreateUploader("uploads")
	if err != nil {
		panic(err)
	}

	// Set up HTTP routes
	mux := http.NewServeMux()

	// Handle both GET and POST on the same route
	mux.Handle("/files/", uploader)

	// Or handle them separately
	mux.Handle("/uploads/", uploader.PostHandler()) // POST for uploads
	mux.Handle("/download/", uploader.GetHandler()) // GET for downloads

	// Get a file URL
	fileURL := uploader.GetFileURL("example.jpg")
	fmt.Println("File URL:", fileURL)

	// Output: File URL: /files/example.jpg
}

// Example showing how to create an uploader manually
func ExampleNewUploader() {
	// Create a URL mapper and filesystem
	urlMap := map[string]string{"uploads": "/files"}
	urlMapper := vfs.NewURLMapper(urlMap)

	registry := vfs.NewRegistry(urlMapper)
	registry.AddPath("uploads", "/tmp/uploads")

	// Get the filesystem
	fs, _ := registry.Get("uploads")

	// Create uploader manually
	uploader, err := vfs.NewUploader(fs, "/files")
	if err != nil {
		panic(err)
	}

	// Use the uploader
	fmt.Println("Uploader ready for:", uploader.GetFileURL("test.txt"))

	// Output: Uploader ready for: /files/test.txt
}

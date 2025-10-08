package bookmarks

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func TestResizeScreenshot(t *testing.T) {
	// Create paths for test files
	sourcePath := filepath.Join("/tmp", "test_source.jpg")
	destPath := filepath.Join("/tmp", "test_resized.jpg")

	// Ensure cleanup happens regardless of test outcome
	defer func() {
		os.Remove(sourcePath)
		os.Remove(destPath)
	}()

	// Create a 100x100 test image
	sourceImg := image.NewRGBA(image.Rect(0, 0, 100, 100))

	// Fill with a simple pattern (alternating colors)
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			if (x+y)%2 == 0 {
				sourceImg.Set(x, y, color.RGBA{255, 0, 0, 255}) // Red
			} else {
				sourceImg.Set(x, y, color.RGBA{0, 255, 0, 255}) // Green
			}
		}
	}

	// Save the source image
	sourceFile, err := os.Create(sourcePath)
	if err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}
	defer sourceFile.Close()

	if err := jpeg.Encode(sourceFile, sourceImg, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("Failed to encode source image: %v", err)
	}
	sourceFile.Close()

	// Create screenshot service
	service := NewScreenshotService("/tmp", "", true)

	// Test the resize function
	err = service.ResizeScreenshot(sourcePath, destPath, 16, 16)
	if err != nil {
		t.Fatalf("ResizeScreenshot failed: %v", err)
	}

	// Verify the resized image exists and has correct dimensions
	destFile, err := os.Open(destPath)
	if err != nil {
		t.Fatalf("Failed to open destination file: %v", err)
	}
	defer destFile.Close()

	// Decode and check dimensions
	resizedImg, _, err := image.Decode(destFile)
	if err != nil {
		t.Fatalf("Failed to decode resized image: %v", err)
	}

	bounds := resizedImg.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	if width != 16 {
		t.Errorf("Expected width 16, got %d", width)
	}
	if height != 16 {
		t.Errorf("Expected height 16, got %d", height)
	}

	t.Logf("Successfully resized image from 100x100 to %dx%d", width, height)
}

func TestResizeScreenshotDisabled(t *testing.T) {
	// Create screenshot service with disabled state
	service := NewScreenshotService("/tmp", "", false)

	// Test should return error when service is disabled
	err := service.ResizeScreenshot("/tmp/nonexistent.jpg", "/tmp/output.jpg", 16, 16)
	if err == nil {
		t.Error("Expected error when service is disabled, got nil")
	}

	expectedError := "screenshot service is disabled"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

func TestResizeScreenshotInvalidSource(t *testing.T) {
	service := NewScreenshotService("/tmp", "", true)

	// Test with non-existent source file
	err := service.ResizeScreenshot("/tmp/nonexistent.jpg", "/tmp/output.jpg", 16, 16)
	if err == nil {
		t.Error("Expected error with non-existent source file, got nil")
	}

	// Cleanup in case output file was somehow created
	defer os.Remove("/tmp/output.jpg")
}

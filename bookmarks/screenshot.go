package bookmarks

import (
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
)

type ScreenshotService struct {
	screenshotDir string
	gowitnessBin  string
	enabled       bool
}

func NewScreenshotService(screenshotDir, gowitnessBin string, enabled bool) *ScreenshotService {
	return &ScreenshotService{
		screenshotDir: screenshotDir,
		gowitnessBin:  gowitnessBin,
		enabled:       enabled,
	}
}

// ScreenshotResult contains the results of taking a screenshot
type ScreenshotResult struct {
	ScreenshotPath string
	IconPath       string
	JSONPath       string
	Title          string
	Filename       string
	IconFilename   string
}

// GoWitnessOutput represents the JSON structure output by gowitness
type GoWitnessOutput struct {
	URL           string `json:"url"`
	Title         string `json:"title"`
	FinalURL      string `json:"final_url"`
	StatusCode    int    `json:"status_code"`
	ContentLength int    `json:"content_length"`
	Technologies  []struct {
		Name       string   `json:"name"`
		Version    string   `json:"version"`
		Categories []string `json:"categories"`
	} `json:"technologies"`
}

// TakeScreenshot takes a screenshot of the given URL using gowitness binary
func (s *ScreenshotService) TakeScreenshot(url, bookmarkID string) (*ScreenshotResult, error) {
	if !s.enabled {
		slog.Debug("screenshot service disabled")
		return nil, nil
	}

	if s.gowitnessBin == "" {
		return nil, fmt.Errorf("gowitness binary not configured")
	}

	if s.screenshotDir == "" {
		return nil, fmt.Errorf("screenshot directory not configured")
	}

	// Ensure screenshot directory exists
	if err := os.MkdirAll(s.screenshotDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create screenshot directory: %w", err)
	}

	// Generate final filenames based on bookmark ID
	filename := fmt.Sprintf("%s.jpg", bookmarkID)
	iconFilename := fmt.Sprintf("%s-256x160.jpg", bookmarkID)
	jsonFilename := fmt.Sprintf("%s.json", bookmarkID)
	finalImagePath := filepath.Join(s.screenshotDir, filename)
	finalIconPath := filepath.Join(s.screenshotDir, iconFilename)
	finalJSONPath := filepath.Join(s.screenshotDir, jsonFilename)

	// Create temporary directory for gowitness output
	tempDir := filepath.Join(s.screenshotDir, bookmarkID)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Ensure temp directory is cleaned up
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			slog.Warn("failed to remove temp directory", "path", tempDir, "error", err)
		}
	}()

	// Run gowitness command with JSONL output for metadata
	cmd := exec.Command(s.gowitnessBin, "scan", "single",
		"--url", url,
		"--screenshot-path", tempDir,
		"--screenshot-format", "jpeg",
		"--chrome-window-x", "1280",
		"--chrome-window-y", "939", // this is how we get 800px height unfortunately
		"--javascript-file", "./bookmarks/accept.js",
		"--write-jsonl",
		"--write-jsonl-file", finalJSONPath)

	// Log the exact command being executed for debugging
	slog.Info("executing gowitness command",
		"binary", s.gowitnessBin,
		"args", cmd.Args,
		"url", url,
		"tempDir", tempDir)

	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("gowitness command failed",
			"error", err,
			"output", string(output),
			"command", cmd.String(),
			"args", cmd.Args)
		return nil, fmt.Errorf("failed to take screenshot: %w", err)
	}

	// Find the generated screenshot file (gowitness generates its own filename)
	tempFiles, err := filepath.Glob(filepath.Join(tempDir, "*.jpeg"))
	if err != nil || len(tempFiles) == 0 {
		return nil, fmt.Errorf("no screenshot file generated in temp directory")
	}
	tempImagePath := tempFiles[0]

	// Move screenshot to final location
	if err := os.Rename(tempImagePath, finalImagePath); err != nil {
		return nil, fmt.Errorf("failed to move screenshot: %w", err)
	}

	// Create the 256x160 icon from the screenshot
	if err := s.ResizeScreenshot(finalImagePath, finalIconPath, 256, 160); err != nil {
		slog.Warn("failed to create icon", "error", err, "source", finalImagePath, "dest", finalIconPath)
		// Continue without icon - this is not a fatal error
	}

	// Read and parse the JSON file that was written directly to final location
	jsonData, err := os.ReadFile(finalJSONPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read JSON file: %w", err)
	}

	// Parse the JSON output to extract metadata
	var goWitnessData GoWitnessOutput
	if err := json.Unmarshal(jsonData, &goWitnessData); err != nil {
		slog.Error("failed to parse JSON output", "error", err, "output", string(jsonData))
		// Continue with fallback title
		goWitnessData.Title = url
	}

	title := goWitnessData.Title
	if title == "" {
		title = url // fallback to URL if title is empty
	}

	result := &ScreenshotResult{
		ScreenshotPath: finalImagePath,
		IconPath:       finalIconPath,
		JSONPath:       finalJSONPath,
		Title:          title,
		Filename:       filename,
		IconFilename:   iconFilename,
	}

	slog.Info("screenshot taken", "url", url, "path", finalImagePath, "jsonPath", finalJSONPath, "title", title)
	return result, nil
}

// DeleteScreenshot removes a screenshot file and its associated JSON metadata
func (s *ScreenshotService) DeleteScreenshot(screenshotPath string) error {
	if !s.enabled || screenshotPath == "" {
		return nil
	}

	// Security check: ensure path is within screenshot directory
	if !strings.HasPrefix(screenshotPath, s.screenshotDir) {
		return fmt.Errorf("screenshot path outside of configured directory")
	}

	// Delete the screenshot image
	if err := os.Remove(screenshotPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete screenshot: %w", err)
	}

	// Delete the associated icon file
	iconPath := strings.TrimSuffix(screenshotPath, ".jpg") + "-256x160.jpg"
	if err := os.Remove(iconPath); err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to delete icon", "path", iconPath, "error", err)
	}

	// Delete the associated JSON file
	jsonPath := strings.TrimSuffix(screenshotPath, ".jpg") + ".json"
	if err := os.Remove(jsonPath); err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to delete JSON metadata", "path", jsonPath, "error", err)
	}

	slog.Info("screenshot deleted", "path", screenshotPath, "jsonPath", jsonPath)
	return nil
}

// GetWebPath returns the web-accessible path for a screenshot
func (s *ScreenshotService) GetWebPath(screenshotPath string) string {
	if screenshotPath == "" {
		return ""
	}

	// Convert absolute filesystem path to relative web path
	relPath, err := filepath.Rel(s.screenshotDir, screenshotPath)
	if err != nil {
		slog.Error("failed to get relative path", "error", err, "path", screenshotPath)
		return ""
	}

	// Return web-accessible path
	return filepath.Join("/static/screenshots", relPath)
}

// ResizeScreenshot creates a resized version of a screenshot at the specified dimensions
func (s *ScreenshotService) ResizeScreenshot(sourcePath, destPath string, width, height int) error {
	if !s.enabled {
		return fmt.Errorf("screenshot service is disabled")
	}

	// Open the source image
	srcFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to open source image: %w", err)
	}
	defer srcFile.Close()

	// Decode the source image
	srcImg, _, err := image.Decode(srcFile)
	if err != nil {
		return fmt.Errorf("failed to decode source image: %w", err)
	}

	// Create the destination image with the specified dimensions
	dstImg := image.NewRGBA(image.Rect(0, 0, width, height))

	// Resize the image using high-quality BiLinear scaling
	draw.BiLinear.Scale(dstImg, dstImg.Bounds(), srcImg, srcImg.Bounds(), draw.Over, nil)

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Create the destination file
	dstFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dstFile.Close()

	// Encode and save the resized image as JPEG
	if err := jpeg.Encode(dstFile, dstImg, &jpeg.Options{Quality: 85}); err != nil {
		return fmt.Errorf("failed to encode resized image: %w", err)
	}

	slog.Info("screenshot resized", "source", sourcePath, "dest", destPath, "width", width, "height", height)
	return nil
}

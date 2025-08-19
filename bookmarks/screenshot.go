package bookmarks

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// TakeScreenshot takes a screenshot of the given URL using gowitness binary
func (s *ScreenshotService) TakeScreenshot(url, bookmarkID string) (string, error) {
	if !s.enabled {
		slog.Debug("screenshot service disabled")
		return "", nil
	}

	if s.gowitnessBin == "" {
		return "", fmt.Errorf("gowitness binary not configured")
	}

	if s.screenshotDir == "" {
		return "", fmt.Errorf("screenshot directory not configured")
	}

	// Ensure screenshot directory exists
	if err := os.MkdirAll(s.screenshotDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create screenshot directory: %w", err)
	}

	// Generate filename based on bookmark ID
	filename := fmt.Sprintf("%s.png", bookmarkID)
	outputPath := filepath.Join(s.screenshotDir, filename)

	// Run gowitness command
	cmd := exec.Command(s.gowitnessBin, "single", "--url", url, "--screenshot-path", outputPath)
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("gowitness command failed", "error", err, "output", string(output))
		return "", fmt.Errorf("failed to take screenshot: %w", err)
	}

	slog.Info("screenshot taken", "url", url, "path", outputPath)
	return outputPath, nil
}

// DeleteScreenshot removes a screenshot file
func (s *ScreenshotService) DeleteScreenshot(screenshotPath string) error {
	if !s.enabled || screenshotPath == "" {
		return nil
	}

	// Security check: ensure path is within screenshot directory
	if !strings.HasPrefix(screenshotPath, s.screenshotDir) {
		return fmt.Errorf("screenshot path outside of configured directory")
	}

	if err := os.Remove(screenshotPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete screenshot: %w", err)
	}

	slog.Info("screenshot deleted", "path", screenshotPath)
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
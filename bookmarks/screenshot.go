package bookmarks

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/image/draw"
)

type ScreenshotService struct {
	screenshotDir string
	gowitnessBin  string
	chromePath    string
	enabled       bool
}

func NewScreenshotService(screenshotDir, gowitnessBin string, enabled bool) *ScreenshotService {
	return &ScreenshotService{
		screenshotDir: screenshotDir,
		gowitnessBin:  gowitnessBin,
		enabled:       enabled,
	}
}

// WithChromePath sets the browser binary gowitness is told to use; empty leaves
// the choice to gowitness.
func (s *ScreenshotService) WithChromePath(path string) *ScreenshotService {
	s.chromePath = path
	return s
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
	Failed        bool   `json:"failed"`
	FailedReason  string `json:"failed_reason"`
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

	// Pages should either screenshot quickly or fail. The default chromedp driver
	// occasionally hangs on pages that screenshot fine with gowitness' other
	// driver (rod), so if a driver produces nothing, whether it ran and found no
	// page or crashed outright, we try the other one.
	var tempFiles []string
	var reasons []string
	for _, driver := range []string{"chromedp", "gorod"} {
		// the jsonl writer appends; start each attempt with a clean file
		os.Remove(finalJSONPath)

		start := time.Now()
		output, err := s.runGowitness(url, driver, tempDir, finalJSONPath)
		var reason string
		if err != nil {
			// gowitness failed to run at all (bad setup, no browser, killed...).
			// It reports why in prose after a dump of its usage text.
			reason = commandFailureReason(output, err)
			slog.Warn("gowitness command failed", "driver", driver, "url", url, "took", time.Since(start).Round(time.Millisecond),
				"error", err, "reason", reason, "output_tail", tailText(output, 800))
		} else {
			// gowitness exits 0 even when the scan fails (eg. a timeout); the reason is
			// only in its output and in the jsonl it wrote, so surface both
			slog.Debug("gowitness output", "driver", driver, "url", url, "output", string(output))

			// Find the generated screenshot file (gowitness generates its own filename)
			tempFiles, _ = filepath.Glob(filepath.Join(tempDir, "*.jpeg"))
			if len(tempFiles) > 0 {
				break
			}
			reason = failureReason(finalJSONPath, output)
			slog.Warn("gowitness produced no screenshot",
				"driver", driver, "url", url, "took", time.Since(start).Round(time.Millisecond),
				"reason", reason, "output", string(output))
		}
		reasons = append(reasons, driver+": "+reason)
	}
	if len(tempFiles) == 0 {
		os.Remove(finalJSONPath) // don't leave orphaned metadata for a bookmark with no screenshot
		return nil, fmt.Errorf("no screenshot file generated (gowitness: %s)", strings.Join(reasons, "; "))
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

const (
	// per-attempt page timeout handed to gowitness
	gowitnessTimeoutSecs = 12
	// hard cap on a gowitness process, in case it hangs outside its own timeout
	gowitnessKillAfter = 45 * time.Second
)

// runGowitness runs a single gowitness scan with the given driver, returning
// its combined output.
func (s *ScreenshotService) runGowitness(url, driver, tempDir, jsonPath string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gowitnessKillAfter)
	defer cancel()

	args := []string{"scan", "single",
		"--url", url,
		"--driver", driver,
		"--timeout", strconv.Itoa(gowitnessTimeoutSecs),
		"--screenshot-path", tempDir,
		"--screenshot-format", "jpeg",
		"--chrome-window-x", "1280",
		"--chrome-window-y", "939", // this is how we get 800px height unfortunately
		"--javascript-file", "./bookmarks/accept.js",
		// without this some scanning errors will be completely silent
		"--log-scan-errors",
		"--write-jsonl",
		"--write-jsonl-file", jsonPath}
	if s.chromePath != "" {
		args = append(args, "--chrome-path", s.chromePath)
	}
	cmd := exec.CommandContext(ctx, s.gowitnessBin, args...)

	slog.Info("executing gowitness command", "args", cmd.Args, "url", url, "tempDir", tempDir)
	return cmd.CombinedOutput()
}

// failureReason works out why gowitness produced no screenshot: timeouts are
// recorded in the jsonl, while navigation errors (dns, refused, ...) only show
// up as an ERRO line in its output.
func failureReason(jsonPath string, output []byte) string {
	var failed GoWitnessOutput
	if data, err := os.ReadFile(jsonPath); err == nil && json.Unmarshal(data, &failed) == nil && failed.FailedReason != "" {
		return failed.FailedReason
	}
	if r := lastErroLine(output); r != "" {
		return r
	}
	return "unknown"
}

// lastErroLine is the reason from the last "ERRO ... err=..." line gowitness logged.
func lastErroLine(output []byte) string {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], " ERRO ") {
			if _, errPart, ok := strings.Cut(lines[i], "err="); ok {
				return strings.Trim(errPart, `"`)
			}
			return strings.TrimSpace(lines[i])
		}
	}
	return ""
}

// commandFailureReason explains a gowitness run that exited non-zero. cobra prints
// the command's whole usage text and then "The error was:" followed by the
// message, wrapped to a fixed width and padded; older gowitness versions can
// leave that message empty.
func commandFailureReason(output []byte, err error) string {
	if _, after, ok := strings.Cut(string(output), "The error was:"); ok {
		var parts []string
		for _, line := range strings.Split(after, "\n") {
			if l := strings.TrimSpace(line); l != "" {
				parts = append(parts, l)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
		return fmt.Sprintf("%v, and gowitness gave no error text (run the command by hand with -D to see why)", err)
	}
	if r := lastErroLine(output); r != "" {
		return r
	}
	if strings.Contains(err.Error(), "killed") {
		return fmt.Sprintf("killed after %s without finishing", gowitnessKillAfter)
	}
	return err.Error()
}

// tailText is the last n bytes of output, trimmed, for logging without the
// usage text gowitness prints ahead of its error.
func tailText(output []byte, n int) string {
	text := strings.TrimSpace(string(output))
	if len(text) <= n {
		return text
	}
	return "..." + text[len(text)-n:]
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

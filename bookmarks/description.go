package bookmarks

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/net/html"
)

// GetDescription extracts the meta description from a Bookmark's JSON file
// Returns the content of the meta description tag, or empty string if not found
func (s *BookmarkService) GetDescription(b *Bookmark) (string, error) {
	if b.ScreenshotPath == "" {
		return "", nil // No screenshot path means no JSON file
	}

	// Construct JSON file path from screenshot path
	// Screenshot path is like "/path/to/screenshot.jpg", JSON is "/path/to/screenshot.json"
	jsonPath := strings.TrimSuffix(b.ScreenshotPath, ".jpg") + ".json"

	// Check if JSON file exists
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		return "", nil // JSON file doesn't exist, return empty string
	}

	// Open and read from file
	file, err := os.Open(jsonPath)
	if err != nil {
		return "", fmt.Errorf("failed to open JSON file %s: %w", jsonPath, err)
	}
	defer file.Close()

	return GetDescriptionFromReader(file)
}

// GetDescriptionFromPath extracts the meta description from a JSON file at the given path
// This is a utility function that can be used when you have the JSON file path directly
func GetDescriptionFromPath(jsonPath string) (string, error) {
	// Check if JSON file exists
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		return "", nil // JSON file doesn't exist, return empty string
	}

	// Open and read from file
	file, err := os.Open(jsonPath)
	if err != nil {
		return "", fmt.Errorf("failed to open JSON file %s: %w", jsonPath, err)
	}
	defer file.Close()

	return GetDescriptionFromReader(file)
}

// GetDescriptionFromReader extracts the meta description from JSON data provided by an io.Reader
// The JSON is expected to have an "html" field containing HTML content
func GetDescriptionFromReader(r io.Reader) (string, error) {
	// Parse JSON to extract HTML
	var data struct {
		HTML string `json:"html"`
	}
	if err := json.NewDecoder(r).Decode(&data); err != nil {
		return "", fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Parse HTML and extract meta description
	return extractMetaDescriptionFromHTML(data.HTML)
}

// extractMetaDescriptionFromHTML parses HTML content and extracts the meta description
func extractMetaDescriptionFromHTML(htmlContent string) (string, error) {
	// Parse the HTML content
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Find the meta description tag
	description := findMetaDescription(doc)
	return strings.TrimSpace(description), nil
}

// findMetaDescription recursively searches for meta description tag in HTML nodes
func findMetaDescription(n *html.Node) string {
	// Check if this node is a meta tag with name="description"
	if n.Type == html.ElementNode && n.Data == "meta" {
		var isDescription bool
		var content string

		// Check attributes for name="description" and content="..."
		for _, attr := range n.Attr {
			if strings.ToLower(attr.Key) == "name" && strings.ToLower(attr.Val) == "description" {
				isDescription = true
			}
			if strings.ToLower(attr.Key) == "content" {
				content = attr.Val
			}
		}

		// If both name="description" and content are found, return the content
		if isDescription && content != "" {
			return content
		}
	}

	// Recursively search child nodes
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if result := findMetaDescription(c); result != "" {
			return result
		}
	}

	return ""
}

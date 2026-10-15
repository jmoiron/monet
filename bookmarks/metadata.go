package bookmarks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
	"golang.org/x/net/publicsuffix"
)

const (
	metadataTimeout = 20 * time.Second
	maxPageBytes    = 4 << 20
	maxImageBytes   = 10 << 20

	// a current desktop Chrome; many sites serve bot-ish or empty responses
	// to anything that doesn't look like a real browser
	browserUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
)

// PageMetadata is the descriptive information extracted from a web page.
type PageMetadata struct {
	Title       string
	Description string
	SiteName    string
	ImageURL    string // absolute URL of og:image (or equivalent)
	FinalURL    string // URL after redirects
}

// newBrowserClient returns an http.Client that keeps cookies and follows
// redirects like a browser does; some sites bounce through a cookie-setting
// redirect and loop forever without a jar.
func newBrowserClient() *http.Client {
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	return &http.Client{Jar: jar, Timeout: metadataTimeout}
}

// setBrowserHeaders makes a request look like a top-level browser navigation.
// Accept-Encoding is deliberately left unset: Go only transparently
// decompresses gzip when it sets that header itself, and we can't decode br/zstd.
func setBrowserHeaders(req *http.Request, navigation bool) {
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if navigation {
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
		req.Header.Set("Upgrade-Insecure-Requests", "1")
		req.Header.Set("Sec-Fetch-Dest", "document")
		req.Header.Set("Sec-Fetch-Mode", "navigate")
		req.Header.Set("Sec-Fetch-Site", "none")
		req.Header.Set("Sec-Fetch-User", "?1")
	} else {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
		req.Header.Set("Sec-Fetch-Dest", "image")
		req.Header.Set("Sec-Fetch-Mode", "no-cors")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
	}
}

// FetchMetadata loads pageURL like a browser would and extracts its metadata.
func FetchMetadata(ctx context.Context, client *http.Client, pageURL string) (*PageMetadata, error) {
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	setBrowserHeaders(req, true)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", pageURL, err)
	}
	defer resp.Body.Close()

	// Some sites (e.g. behind bot protection) return 403 with a body that
	// still carries useful meta tags, so only give up if we can't parse it.
	ct := resp.Header.Get("Content-Type")
	if ct != "" {
		if mt, _, err := mime.ParseMediaType(ct); err == nil &&
			mt != "text/html" && mt != "application/xhtml+xml" {
			return nil, fmt.Errorf("%s: not an html page (%s)", pageURL, mt)
		}
	}

	body := io.LimitReader(resp.Body, maxPageBytes)
	r, err := charset.NewReader(body, ct)
	if err != nil {
		return nil, fmt.Errorf("decoding charset: %w", err)
	}

	m, err := parseMetadata(r, resp.Request.URL)
	if err != nil {
		return nil, err
	}
	m.FinalURL = resp.Request.URL.String()
	if resp.StatusCode >= 400 && m.Title == "" && m.Description == "" && m.ImageURL == "" {
		return nil, fmt.Errorf("%s: http status %d", pageURL, resp.StatusCode)
	}
	return m, nil
}

// parseMetadata extracts title, description, site name and image from html.
// Relative image URLs are resolved against base.
func parseMetadata(r io.Reader, base *url.URL) (*PageMetadata, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("parsing html: %w", err)
	}

	meta := map[string]string{} // lowercased property/name -> first content
	var docTitle string
	var jsonLD []string

	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "title":
				if docTitle == "" && n.FirstChild != nil {
					docTitle = n.FirstChild.Data
				}
			case "script":
				for _, a := range n.Attr {
					if strings.ToLower(a.Key) == "type" && strings.EqualFold(a.Val, "application/ld+json") && n.FirstChild != nil {
						jsonLD = append(jsonLD, n.FirstChild.Data)
					}
				}
			case "meta":
				var key, content string
				for _, a := range n.Attr {
					switch strings.ToLower(a.Key) {
					case "property", "name":
						if key == "" {
							key = strings.ToLower(strings.TrimSpace(a.Val))
						}
					case "content":
						content = a.Val
					}
				}
				if key != "" && strings.TrimSpace(content) != "" {
					if _, ok := meta[key]; !ok {
						meta[key] = strings.TrimSpace(content)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	first := func(keys ...string) string {
		for _, k := range keys {
			if v := meta[k]; v != "" {
				return v
			}
		}
		return ""
	}

	m := &PageMetadata{
		Title:       first("og:title", "twitter:title"),
		Description: first("og:description", "description", "twitter:description"),
		SiteName:    first("og:site_name", "application-name", "apple-mobile-web-app-title"),
	}
	if m.SiteName == "" {
		m.SiteName = jsonLDSiteName(jsonLD)
	}
	if m.SiteName == "" {
		// last resort: the hostname, which is at least recognizable
		m.SiteName = strings.TrimPrefix(strings.ToLower(base.Hostname()), "www.")
	}
	if m.Title == "" {
		m.Title = strings.Join(strings.Fields(docTitle), " ")
	}

	if img := first("og:image", "og:image:url", "og:image:secure_url", "twitter:image", "twitter:image:src"); img != "" {
		if u, err := base.Parse(img); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
			m.ImageURL = u.String()
		}
	}
	return m, nil
}

// jsonLDSiteName looks for a schema.org WebSite (or Organization) name in
// JSON-LD blocks, which may be an object, an array, or have an @graph.
func jsonLDSiteName(blocks []string) string {
	var find func(v any) string
	find = func(v any) string {
		switch t := v.(type) {
		case []any:
			for _, e := range t {
				if s := find(e); s != "" {
					return s
				}
			}
		case map[string]any:
			if typ, _ := t["@type"].(string); typ == "WebSite" {
				if name, _ := t["name"].(string); strings.TrimSpace(name) != "" {
					return strings.TrimSpace(name)
				}
			}
			if g, ok := t["@graph"]; ok {
				return find(g)
			}
		}
		return ""
	}
	for _, b := range blocks {
		var v any
		if err := json.Unmarshal([]byte(b), &v); err == nil {
			if s := find(v); s != "" {
				return s
			}
		}
	}
	return ""
}

// DownloadImage fetches imageURL and saves it as <dir>/<name>.<ext>, where ext
// is chosen from the sniffed content type. It returns the saved path.
func DownloadImage(ctx context.Context, client *http.Client, imageURL, referer, dir, name string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return "", err
	}
	setBrowserHeaders(req, false)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching image: http status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return "", fmt.Errorf("reading image: %w", err)
	}
	if len(data) > maxImageBytes {
		return "", fmt.Errorf("image larger than %d bytes", maxImageBytes)
	}

	ext := imageExtension(http.DetectContentType(data), data)
	if ext == "" {
		return "", fmt.Errorf("not a supported image type")
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name+ext)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}
	return path, nil
}

func imageExtension(sniffed string, data []byte) string {
	switch {
	case strings.HasPrefix(sniffed, "image/jpeg"):
		return ".jpg"
	case strings.HasPrefix(sniffed, "image/png"):
		return ".png"
	case strings.HasPrefix(sniffed, "image/gif"):
		return ".gif"
	case strings.HasPrefix(sniffed, "image/webp"):
		return ".webp"
	case strings.Contains(string(data[:min(len(data), 512)]), "<svg"):
		return ".svg"
	}
	return ""
}

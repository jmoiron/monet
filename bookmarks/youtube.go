package bookmarks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// YouTube serves different pages to different clients: from a datacenter address
// the watch page can be a bare skeleton (title "- YouTube", no og:image, a generic
// description), so for YouTube links we don't depend on the page. The video id in
// the URL is enough to build the thumbnail's address, and oEmbed, a small public
// API, gives the title.

// the addresses used, as variables so that tests can point them elsewhere
var (
	youtubeOEmbedURL = "https://www.youtube.com/oembed"
	youtubeThumbBase = "https://i.ytimg.com"
)

// thumbnail files, best first; the larger ones don't exist for every video
var youtubeThumbnails = []string{"maxresdefault", "sddefault", "hqdefault"}

var youtubeIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// youtubeVideoID extracts the video id from the usual forms of YouTube link:
// watch?v=, youtu.be/, /shorts/, /embed/, /live/ and /v/.
func youtubeVideoID(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")

	var id string
	switch host {
	case "youtu.be":
		id = firstPathSegment(u.Path)
	case "youtube.com", "m.youtube.com", "music.youtube.com", "youtube-nocookie.com":
		if u.Path == "/watch" {
			id = u.Query().Get("v")
			break
		}
		for _, prefix := range []string{"/shorts/", "/embed/", "/live/", "/v/"} {
			if rest, ok := strings.CutPrefix(u.Path, prefix); ok {
				id = firstPathSegment(rest)
				break
			}
		}
	}
	return id, youtubeIDPattern.MatchString(id)
}

func firstPathSegment(p string) string {
	p = strings.TrimPrefix(p, "/")
	seg, _, _ := strings.Cut(p, "/")
	return seg
}

// FetchCardMetadata is FetchMetadata with special handling for YouTube links.
func FetchCardMetadata(ctx context.Context, client *http.Client, pageURL string) (*PageMetadata, error) {
	page, err := FetchMetadata(ctx, client, pageURL)
	if id, ok := youtubeVideoID(pageURL); ok {
		return enrichYouTube(ctx, client, id, page, err)
	}
	return page, err
}

// enrichYouTube decides between the page we fetched and what we can work out
// from the video id. A page that has the video's own thumbnail is a real watch
// page and has everything, description included, so it's used as it is.
// Otherwise the page is ignored, since even its description is likely to be
// YouTube's generic one.
func enrichYouTube(ctx context.Context, client *http.Client, id string, page *PageMetadata, pageErr error) (*PageMetadata, error) {
	if pageErr == nil && page != nil && strings.Contains(page.ImageURL, "/vi/"+id+"/") {
		return page, nil
	}

	slog.Info("youtube page had no usable metadata, using oEmbed and the thumbnail address",
		"id", id, "page_error", pageErr, "page_title", titleOf(page), "page_image", imageOf(page))

	m := &PageMetadata{
		SiteName: "YouTube",
		FinalURL: "https://www.youtube.com/watch?v=" + id,
	}
	if o, err := youtubeOEmbed(ctx, client, id); err != nil {
		slog.Info("youtube oEmbed", "id", id, "error", err)
	} else {
		m.Title = o
	}
	m.ImageURL = youtubeThumbnail(ctx, client, id)

	if m.Title == "" && m.ImageURL == "" {
		return nil, fmt.Errorf("youtube video %s: no metadata available (private or removed, or YouTube is not answering this server)", id)
	}
	return m, nil
}

func titleOf(m *PageMetadata) string {
	if m == nil {
		return ""
	}
	return m.Title
}

func imageOf(m *PageMetadata) string {
	if m == nil {
		return ""
	}
	return m.ImageURL
}

// youtubeOEmbed returns the video's title from YouTube's oEmbed endpoint.
func youtubeOEmbed(ctx context.Context, client *http.Client, id string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()

	q := url.Values{"format": {"json"}, "url": {"https://www.youtube.com/watch?v=" + id}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, youtubeOEmbedURL+"?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("oEmbed: http status %d", resp.StatusCode)
	}
	var o struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&o); err != nil {
		return "", fmt.Errorf("oEmbed: %w", err)
	}
	return strings.TrimSpace(o.Title), nil
}

// youtubeThumbnail is the address of the best thumbnail that exists for the
// video, or "" if none does. Missing ones are a plain 404.
func youtubeThumbnail(ctx context.Context, client *http.Client, id string) string {
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()

	for _, name := range youtubeThumbnails {
		addr := fmt.Sprintf("%s/vi/%s/%s.jpg", youtubeThumbBase, id, name)
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, addr, nil)
		if err != nil {
			continue
		}
		setBrowserHeaders(req, false)
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return addr
		}
	}
	return ""
}

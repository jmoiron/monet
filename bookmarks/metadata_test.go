package bookmarks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestParseMetadata(t *testing.T) {
	base, _ := url.Parse("https://example.com/a/b")
	page := `<html><head><title> Fallback
	  Title </title>
	<meta property="og:title" content="OG Title">
	<meta name="description" content="plain desc">
	<meta property="og:site_name" content="Example">
	<meta property="og:image" content="/img/x.png">
	</head></html>`
	m, err := parseMetadata(strings.NewReader(page), base)
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "OG Title" || m.Description != "plain desc" || m.SiteName != "Example" ||
		m.ImageURL != "https://example.com/img/x.png" {
		t.Errorf("unexpected metadata: %+v", m)
	}

	m, _ = parseMetadata(strings.NewReader(`<title> Just   a title </title>`), base)
	if m.Title != "Just a title" {
		t.Errorf("title fallback: %q", m.Title)
	}
}

func TestFetchMetadataBrowserHeaders(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("x", 32))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/img.png" {
			if r.Header.Get("Referer") == "" {
				http.Error(w, "no referer", 403)
				return
			}
			w.Write(png)
			return
		}
		if !strings.Contains(r.UserAgent(), "Chrome") || r.Header.Get("Sec-Fetch-Mode") != "navigate" {
			http.Error(w, "bot", 403)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<title>T</title><meta property="og:image" content="/img.png">`))
	}))
	defer srv.Close()

	client := newBrowserClient()
	m, err := FetchMetadata(context.Background(), client, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "T" || m.ImageURL != srv.URL+"/img.png" {
		t.Fatalf("unexpected metadata: %+v", m)
	}
	path, err := DownloadImage(context.Background(), client, m.ImageURL, m.FinalURL, t.TempDir(), "x-og")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, "x-og.png") {
		t.Errorf("path = %s", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error(err)
	}
}

func TestParseSiteName(t *testing.T) {
	base, _ := url.Parse("https://www.example.com/a")
	cases := map[string]string{
		`<meta property="og:site_name" content="OG">`:                                              "OG",
		`<meta name="application-name" content="App">`:                                             "App",
		`<script type="application/ld+json">{"@graph":[{"@type":"WebSite","name":"LD"}]}</script>`: "LD",
		`<title>nothing</title>`:                                                                   "example.com",
	}
	for page, want := range cases {
		m, err := parseMetadata(strings.NewReader(page), base)
		if err != nil {
			t.Fatal(err)
		}
		if m.SiteName != want {
			t.Errorf("%s: got %q want %q", page, m.SiteName, want)
		}
	}
}

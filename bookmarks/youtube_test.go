package bookmarks

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestYouTubeVideoID(t *testing.T) {
	const id = "Hfw-GWowH9M"
	for _, c := range []struct {
		url string
		ok  bool
	}{
		{"https://www.youtube.com/watch?v=" + id, true},
		{"https://www.youtube.com/watch?v=" + id + "&t=42s&list=PLabc", true},
		{"https://youtube.com/watch?feature=share&v=" + id, true},
		{"https://m.youtube.com/watch?v=" + id, true},
		{"https://music.youtube.com/watch?v=" + id, true},
		{"https://youtu.be/" + id, true},
		{"https://youtu.be/" + id + "?t=10", true},
		{"https://www.youtube.com/shorts/" + id, true},
		{"https://www.youtube.com/embed/" + id + "?autoplay=1", true},
		{"https://www.youtube-nocookie.com/embed/" + id, true},
		{"https://www.youtube.com/live/" + id + "?feature=share", true},
		{"https://www.youtube.com/v/" + id, true},

		{"https://www.youtube.com/@SomeChannel", false},
		{"https://www.youtube.com/playlist?list=PLabc", false},
		{"https://www.youtube.com/watch", false},
		{"https://www.youtube.com/watch?v=short", false},
		{"https://www.youtube.com/watch?v=" + id + "toolong", false},
		{"https://notyoutube.com/watch?v=" + id, false},
		{"https://example.com/youtu.be/" + id, false},
		{"https://youtube.com.evil.example/watch?v=" + id, false},
		{"not a url at all", false},
	} {
		got, ok := youtubeVideoID(c.url)
		assert.Equal(t, c.ok, ok, c.url)
		if c.ok {
			assert.Equal(t, id, got, c.url)
		}
	}
}

// a stand-in for YouTube's oEmbed endpoint and thumbnail host
type fakeYouTube struct {
	oembedStatus int
	oembedTitle  string
	thumbs       map[string]int // file name -> status; absent means 404
	hits         []string
}

func (f *fakeYouTube) serve(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits = append(f.hits, r.Method+" "+r.URL.Path)
		if r.URL.Path == "/oembed" {
			if f.oembedStatus != 0 && f.oembedStatus != 200 {
				http.Error(w, "no", f.oembedStatus)
				return
			}
			w.Write([]byte(`{"title": "` + f.oembedTitle + `", "provider_name": "YouTube"}`))
			return
		}
		for name, status := range f.thumbs {
			if r.URL.Path == "/vi/Hfw-GWowH9M/"+name+".jpg" {
				w.WriteHeader(status)
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	oldO, oldT := youtubeOEmbedURL, youtubeThumbBase
	youtubeOEmbedURL, youtubeThumbBase = srv.URL+"/oembed", srv.URL
	t.Cleanup(func() { youtubeOEmbedURL, youtubeThumbBase = oldO, oldT })
}

const ytID = "Hfw-GWowH9M"

func TestYouTubeSkeletonPageIsReplacedByVideoMetadata(t *testing.T) {
	assert := assert.New(t)
	f := &fakeYouTube{oembedTitle: "Darktable Episode 67", thumbs: map[string]int{"maxresdefault": 200, "hqdefault": 200}}
	f.serve(t)

	// what the reporter's server got: no og:image, no site name, "- YouTube", and
	// YouTube's generic description, which must not end up on the card
	skeleton := &PageMetadata{Title: "- YouTube", Description: "Enjoy the videos and music you love", FinalURL: "https://www.youtube.com/watch?v=" + ytID}
	m, err := enrichYouTube(context.Background(), newBrowserClient(), ytID, skeleton, nil)
	assert.NoError(err)
	assert.Equal("Darktable Episode 67", m.Title)
	assert.Equal("YouTube", m.SiteName)
	assert.Equal(youtubeThumbBase+"/vi/"+ytID+"/maxresdefault.jpg", m.ImageURL)
	assert.Empty(m.Description, "the skeleton page's generic description is dropped")
	assert.Equal("https://www.youtube.com/watch?v="+ytID, m.FinalURL, "the image is fetched with the video page as referer")
}

func TestYouTubeGoodPageIsUsedAsIs(t *testing.T) {
	assert := assert.New(t)
	f := &fakeYouTube{oembedTitle: "should not be asked", thumbs: map[string]int{"maxresdefault": 200}}
	f.serve(t)

	page := &PageMetadata{Title: "Real Title", Description: "The video's own description", SiteName: "YouTube",
		ImageURL: "https://i.ytimg.com/vi/" + ytID + "/maxresdefault.jpg"}
	m, err := enrichYouTube(context.Background(), newBrowserClient(), ytID, page, nil)
	assert.NoError(err)
	assert.Same(page, m, "a real watch page already has everything, in the page's own words")
	assert.Empty(f.hits, "and nothing else is requested")
}

func TestYouTubeThumbnailFallsBackToSmallerSizes(t *testing.T) {
	f := &fakeYouTube{oembedTitle: "Old video", thumbs: map[string]int{"hqdefault": 200}}
	f.serve(t)
	m, err := enrichYouTube(context.Background(), newBrowserClient(), ytID, nil, errors.New("fetch failed"))
	assert.NoError(t, err)
	assert.Equal(t, youtubeThumbBase+"/vi/"+ytID+"/hqdefault.jpg", m.ImageURL, "no maxres or sd for this one")
	assert.Equal(t, "Old video", m.Title)
}

func TestYouTubeEmbedLinkWithNoPageMetadata(t *testing.T) {
	// /embed/ pages carry no og tags at all
	f := &fakeYouTube{oembedTitle: "Embedded", thumbs: map[string]int{"maxresdefault": 200}}
	f.serve(t)
	m, err := enrichYouTube(context.Background(), newBrowserClient(), ytID, &PageMetadata{Title: "YouTube"}, nil)
	assert.NoError(t, err)
	assert.Equal(t, "Embedded", m.Title)
	assert.NotEmpty(t, m.ImageURL)
}

func TestYouTubePartialResults(t *testing.T) {
	// oEmbed down but the thumbnail is there: still a useful card
	f := &fakeYouTube{oembedStatus: 503, thumbs: map[string]int{"maxresdefault": 200}}
	f.serve(t)
	m, err := enrichYouTube(context.Background(), newBrowserClient(), ytID, nil, nil)
	assert.NoError(t, err)
	assert.Empty(t, m.Title)
	assert.NotEmpty(t, m.ImageURL)

	// and the other way round
	f.oembedStatus, f.oembedTitle, f.thumbs = 200, "Only a title", map[string]int{}
	m, err = enrichYouTube(context.Background(), newBrowserClient(), ytID, nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, "Only a title", m.Title)
	assert.Empty(t, m.ImageURL)
}

func TestYouTubeUnavailableVideoIsAnError(t *testing.T) {
	f := &fakeYouTube{oembedStatus: 404}
	f.serve(t)
	m, err := enrichYouTube(context.Background(), newBrowserClient(), ytID, &PageMetadata{Title: "- YouTube"}, nil)
	assert.Nil(t, m, "not a card titled '- YouTube'")
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), ytID)
	}
}

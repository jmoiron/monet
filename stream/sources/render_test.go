package sources

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderDetailBluesky(t *testing.T) {
	data := `{
		"post": {
			"uri": "at://did:plc:jmoiron/app.bsky.feed.post/3lf7abc",
			"author": {
				"handle": "jmoiron.bsky.social",
				"displayName": "Jason Moiron"
			},
			"record": {
				"$type": "app.bsky.feed.post",
				"text": "hello\nworld",
				"createdAt": "2026-03-19T15:04:05Z",
				"embed": {
					"$type": "app.bsky.embed.external",
					"external": {
						"title": "Example Link",
						"uri": "https://example.com/hello",
						"thumb": {
							"ref": {
								"$link": "bafkrei123"
							}
						}
					}
				},
				"facets": [{
					"features": [{
						"$type": "app.bsky.richtext.facet#link",
						"uri": "https://example.com/hello"
					}],
					"index": {
						"byteStart": 0,
						"byteEnd": 5
					}
				}]
			}
		}
	}`

	templateName, ctx, err := RenderDetail("bluesky", "post", "https://bsky.app/profile/jmoiron.bsky.social/post/3lf7abc", data, "", parseTestTime("2026-03-19T15:04:05Z"))
	require.NoError(t, err)
	assert.Equal(t, "stream/detail/bluesky.html", templateName)
	assert.Equal(t, "Jason Moiron", ctx["display_name"])
	assert.Equal(t, "@jmoiron.bsky.social", ctx["handle"])
	assert.Equal(t, "https://bsky.app/profile/jmoiron.bsky.social", ctx["profile_url"])
	assert.Equal(t, "3:04 PM · Mar 19, 2026", ctx["timestamp_ui"])
	assert.Contains(t, ctx["text"], `<a href="https://example.com/hello">hello</a>`)
	assert.Contains(t, ctx["text"], "\nworld")
	assert.True(t, strings.Contains(ctx["url"].(string), "/post/3lf7abc"))
	embed := ctx["embed"].(map[string]any)
	assert.Equal(t, "Example Link", embed["title"])
	assert.Equal(t, "example.com", embed["domain"])
	assert.Equal(t, "https://cdn.bsky.app/img/feed_thumbnail/plain/did:plc:jmoiron/bafkrei123", embed["image_url"])
}

func TestRenderBlueskyFacetText(t *testing.T) {
	facets := []struct {
		Index struct {
			ByteStart int `json:"byteStart"`
			ByteEnd   int `json:"byteEnd"`
		} `json:"index"`
		Features []struct {
			Type string `json:"$type"`
			URI  string `json:"uri"`
		} `json:"features"`
	}{
		{
			Index: struct {
				ByteStart int `json:"byteStart"`
				ByteEnd   int `json:"byteEnd"`
			}{ByteStart: 6, ByteEnd: 10},
			Features: []struct {
				Type string `json:"$type"`
				URI  string `json:"uri"`
			}{
				{Type: "app.bsky.richtext.facet#link", URI: "https://example.com"},
			},
		},
	}

	rendered := renderBlueskyFacetText("hello link world", facets)
	assert.Equal(t, `hello <a href="https://example.com">link</a> world`, rendered)
}

func parseTestTime(value string) time.Time {
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return ts
}

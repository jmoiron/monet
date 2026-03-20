package sources_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/jmoiron/monet/stream"
	"github.com/jmoiron/monet/stream/sources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlueskySyncImportsPublicFeed(t *testing.T) {
	assert := assert.New(t)

	var authHeader string
	module := sources.NewBluesky().WithClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			authHeader = r.Header.Get("Authorization")
			assert.Equal("/xrpc/app.bsky.feed.getAuthorFeed", r.URL.Path)
			assert.Equal("jmoiron.bsky.social", r.URL.Query().Get("actor"))

			body, err := json.Marshal(map[string]any{
				"feed": []map[string]any{
					{
						"post": map[string]any{
							"uri": "at://did:plc:jmoiron/app.bsky.feed.post/3lf7abc",
							"author": map[string]any{
								"handle":      "jmoiron.bsky.social",
								"displayName": "Jason Moiron",
							},
							"record": map[string]any{
								"$type":     "app.bsky.feed.post",
								"text":      "hello from bluesky",
								"createdAt": "2026-03-19T15:04:05Z",
							},
						},
					},
					{
						"post": map[string]any{
							"uri": "at://did:plc:jmoiron/app.bsky.feed.post/3lf7def",
							"author": map[string]any{
								"handle":      "jmoiron.bsky.social",
								"displayName": "Jason Moiron",
							},
							"record": map[string]any{
								"$type":     "app.bsky.feed.post",
								"text":      "something worth reposting",
								"createdAt": "2026-03-18T10:00:00Z",
							},
						},
						"reason": map[string]any{
							"$type":     "app.bsky.feed.defs#reasonRepost",
							"indexedAt": "2026-03-19T16:00:00Z",
							"by": map[string]any{
								"handle": "jmoiron.bsky.social",
							},
						},
					},
				},
			})
			require.NoError(t, err)

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(string(body))),
			}, nil
		}),
	})

	source := &stream.StreamSource{}
	require.NoError(t, source.SetSettings(map[string]string{
		"actor":       "jmoiron.bsky.social",
		"appview_url": "https://public.api.bsky.test",
	}))

	result, err := module.Sync(context.Background(), source)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(result.Items, 2)
	assert.Equal("", authHeader)

	record0, err := result.Items[0].ToRecord()
	require.NoError(t, err)
	record1, err := result.Items[1].ToRecord()
	require.NoError(t, err)
	assert.Equal("post", record0.Title)
	assert.Equal("repost", record1.Title)
	assert.True(strings.Contains(record0.SummaryRendered, "hello from bluesky"))
	assert.True(strings.Contains(record0.SummaryRendered, `href="https://bsky.app/profile/jmoiron.bsky.social"`))
	assert.True(strings.Contains(record0.SummaryRendered, "@jmoiron.bsky.social"))
	assert.True(strings.Contains(record1.SourceId, "repost:"))
	assert.True(strings.Contains(record1.SummaryRendered, "reposted"))
	assert.False(strings.Contains(record1.SummaryRendered, `<span class="name">`))
	assert.Equal("https://bsky.app/profile/jmoiron.bsky.social/post/3lf7abc", record0.Url)
}

func TestBlueskySummaryFallsBackToImageAlt(t *testing.T) {
	module := sources.NewBluesky().WithClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			body, err := json.Marshal(map[string]any{
				"feed": []map[string]any{
					{
						"post": map[string]any{
							"uri": "at://did:plc:jmoiron/app.bsky.feed.post/3img",
							"author": map[string]any{
								"handle": "jmoiron.bsky.social",
							},
							"record": map[string]any{
								"$type":     "app.bsky.feed.post",
								"text":      "",
								"createdAt": "2026-03-19T15:04:05Z",
								"embed": map[string]any{
									"$type": "app.bsky.embed.images",
									"images": []map[string]any{
										{
											"alt": "example alt text",
											"image": map[string]any{
												"ref": map[string]any{
													"$link": "bafkreiimg123",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			})
			require.NoError(t, err)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(string(body))),
			}, nil
		}),
	})

	source := &stream.StreamSource{}
	require.NoError(t, source.SetSettings(map[string]string{
		"actor":       "jmoiron.bsky.social",
		"appview_url": "https://public.api.bsky.test",
	}))

	result, err := module.Sync(context.Background(), source)
	require.NoError(t, err)
	require.Len(t, result.Items, 1)

	record, err := result.Items[0].ToRecord()
	require.NoError(t, err)
	assert.Contains(t, record.SummaryRendered, "example alt text")
	assert.Contains(t, record.SummaryRendered, "@jmoiron.bsky.social")
}

func TestBlueskyFullSyncUsesMorePages(t *testing.T) {
	assert := assert.New(t)

	requests := 0
	module := sources.NewBluesky().WithClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests++

			cursor := r.URL.Query().Get("cursor")
			page := 1
			switch cursor {
			case "":
				page = 1
			case "page-2":
				page = 2
			case "page-3":
				page = 3
			}

			nextCursor := ""
			if page < 3 {
				nextCursor = "page-" + strconv.Itoa(page+1)
			}

			body, err := json.Marshal(map[string]any{
				"cursor": nextCursor,
				"feed": []map[string]any{
					{
						"post": map[string]any{
							"uri": "at://did:plc:jmoiron/app.bsky.feed.post/page" + strconv.Itoa(page),
							"author": map[string]any{
								"handle": "jmoiron.bsky.social",
							},
							"record": map[string]any{
								"$type":     "app.bsky.feed.post",
								"text":      "page " + strconv.Itoa(page),
								"createdAt": "2026-03-19T15:04:05Z",
							},
						},
					},
				},
			})
			require.NoError(t, err)

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(string(body))),
			}, nil
		}),
	})

	source := &stream.StreamSource{}
	require.NoError(t, source.SetSettings(map[string]string{
		"actor":             "jmoiron.bsky.social",
		"appview_url":       "https://public.api.bsky.test",
		"incremental_pages": "1",
		"full_pages":        "3",
	}))

	result, err := module.Sync(context.Background(), source)
	require.NoError(t, err)
	assert.Len(result.Items, 1)
	assert.Equal(1, requests)

	ctx := stream.WithSyncMode(context.Background(), stream.SyncModeFull)
	result, err = module.Sync(ctx, source)
	require.NoError(t, err)
	assert.Equal(4, requests)
	assert.Len(result.Items, 3)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

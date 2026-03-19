package stream

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStreamDB(t *testing.T) *sqlx.DB {
	t.Helper()

	db, err := sqlx.Connect("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`CREATE TABLE event (
		id integer PRIMARY KEY,
		title text,
		source_id text,
		timestamp datetime,
		type text,
		url text,
		data text,
		summary_rendered text
	);`)
	require.NoError(t, err)

	_, err = db.Exec(`CREATE UNIQUE INDEX event_type_source_id ON event (type, source_id)
		WHERE source_id IS NOT NULL AND source_id <> '';`)
	require.NoError(t, err)

	return db
}

func TestEventServiceUpsertInsertAndUpdate(t *testing.T) {
	assert := assert.New(t)
	db := newTestStreamDB(t)
	service := NewEventService(db)

	first := &Event{
		Title:           "post",
		SourceId:        "at://did:plc:test/app.bsky.feed.post/abc",
		Timestamp:       time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC),
		Type:            "bluesky",
		Url:             "https://bsky.app/profile/test/post/abc",
		Data:            `{"text":"first"}`,
		SummaryRendered: "<div>first</div>",
	}
	require.NoError(t, service.Upsert(first))

	var count int
	require.NoError(t, db.Get(&count, `SELECT count(*) FROM event`))
	assert.Equal(1, count)

	updated := &Event{
		Title:           "reply",
		SourceId:        first.SourceId,
		Timestamp:       first.Timestamp.Add(time.Minute),
		Type:            "bluesky",
		Url:             first.Url,
		Data:            `{"text":"updated"}`,
		SummaryRendered: "<div>updated</div>",
	}
	require.NoError(t, service.Upsert(updated))

	require.NoError(t, db.Get(&count, `SELECT count(*) FROM event`))
	assert.Equal(1, count)

	var got Event
	require.NoError(t, db.Get(&got, `SELECT * FROM event WHERE type='bluesky' AND source_id=?`, first.SourceId))
	assert.Equal("reply", got.Title)
	assert.Equal(`{"text":"updated"}`, got.Data)
	assert.Equal("<div>updated</div>", got.SummaryRendered)
}

func TestBlueskyModuleSyncImportsPublicFeed(t *testing.T) {
	assert := assert.New(t)
	db := newTestStreamDB(t)
	service := NewEventService(db)

	var authHeader string
	module := NewBlueskyModule()
	module.client = &http.Client{
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
	}

	source := &StreamSource{}
	require.NoError(t, source.SetSettings(map[string]string{
		"actor":       "jmoiron.bsky.social",
		"appview_url": "https://public.api.bsky.test",
	}))

	result, err := module.Sync(context.Background(), source, service)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(2, result.Imported)
	assert.Equal("", authHeader)

	var count int
	require.NoError(t, db.Get(&count, `SELECT count(*) FROM event WHERE type='bluesky'`))
	assert.Equal(2, count)

	var events []Event
	require.NoError(t, db.Select(&events, `SELECT * FROM event WHERE type='bluesky' ORDER BY timestamp ASC`))
	assert.Len(events, 2)
	assert.Equal("post", events[0].Title)
	assert.Equal("repost", events[1].Title)
	assert.True(strings.Contains(events[0].SummaryRendered, "hello from bluesky"))
	assert.True(strings.Contains(events[1].SourceId, "repost:"))
	assert.Equal("https://bsky.app/profile/jmoiron.bsky.social/post/3lf7abc", events[0].Url)
}

func TestBlueskyModuleFullSyncUsesMorePages(t *testing.T) {
	assert := assert.New(t)
	db := newTestStreamDB(t)
	service := NewEventService(db)

	requests := 0
	module := NewBlueskyModule()
	module.client = &http.Client{
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
	}

	source := &StreamSource{}
	require.NoError(t, source.SetSettings(map[string]string{
		"actor":             "jmoiron.bsky.social",
		"appview_url":       "https://public.api.bsky.test",
		"incremental_pages": "1",
		"full_pages":        "3",
	}))

	result, err := module.Sync(context.Background(), source, service)
	require.NoError(t, err)
	assert.Equal(1, result.Imported)
	assert.Equal(1, requests)

	ctx := WithSyncMode(context.Background(), SyncModeFull)
	result, err = module.Sync(ctx, source, service)
	require.NoError(t, err)
	assert.Equal(4, requests)
	assert.Equal(3, result.Imported)

	var count int
	require.NoError(t, db.Get(&count, `SELECT count(*) FROM event WHERE type='bluesky'`))
	assert.Equal(3, count)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

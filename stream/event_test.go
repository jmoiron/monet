package stream

import (
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
		summary_rendered text,
		hidden integer NOT NULL DEFAULT 0
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
	assert.False(got.Hidden)
}

func TestEventServiceRerenderByTypeUpdatesHiddenState(t *testing.T) {
	assert := assert.New(t)
	db := newTestStreamDB(t)
	service := NewEventService(db)

	event := &Event{
		Title:           "commit",
		SourceId:        "1111111111111111111111111111111111111111",
		Timestamp:       time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC),
		Type:            "github",
		Url:             "https://github.com/jmoiron/monet/commit/1111111111111111111111111111111111111111",
		Data:            `{"kind":"commit","repo":"jmoiron/monet","ref":"refs/heads/main","commit":{"sha":"1111111111111111111111111111111111111111","author":{"login":"other-user"},"commit":{"message":"upstream sync commit"}},"commit_url":"https://github.com/jmoiron/monet/commit/1111111111111111111111111111111111111111"}`,
		SummaryRendered: "<div>stale</div>",
	}
	require.NoError(t, service.Upsert(event))

	updated, err := service.RerenderByType("github", map[string]string{"username": "jmoiron"})
	require.NoError(t, err)
	assert.Equal(1, updated)

	var got Event
	require.NoError(t, db.Get(&got, `SELECT * FROM event WHERE type='github' AND source_id=?`, event.SourceId))
	assert.True(got.Hidden)
	assert.Contains(got.SummaryRendered, "upstream sync commit")

	updated, err = service.RerenderByType("github", map[string]string{"username": "other-user"})
	require.NoError(t, err)
	assert.Equal(1, updated)

	require.NoError(t, db.Get(&got, `SELECT * FROM event WHERE type='github' AND source_id=?`, event.SourceId))
	assert.False(got.Hidden)
}

func TestEventServiceRerenderByTypeHidesPushEventsWithoutCommits(t *testing.T) {
	assert := assert.New(t)
	db := newTestStreamDB(t)
	service := NewEventService(db)

	event := &Event{
		Title:           "push",
		SourceId:        "push-1",
		Timestamp:       time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC),
		Type:            "github",
		Url:             "https://github.com/jmoiron/monet/commit/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Data:            `{"id":"push-1","type":"PushEvent","created_at":"2026-03-19T12:00:00Z","repo":{"name":"jmoiron/monet"},"payload":{"ref":"refs/heads/main","head":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","commits":[]}}`,
		SummaryRendered: "<div>stale push</div>",
	}
	require.NoError(t, service.Upsert(event))

	updated, err := service.RerenderByType("github", map[string]string{"username": "jmoiron"})
	require.NoError(t, err)
	assert.Equal(1, updated)

	var got Event
	require.NoError(t, db.Get(&got, `SELECT * FROM event WHERE type='github' AND source_id=?`, event.SourceId))
	assert.True(got.Hidden)
	assert.Contains(got.SummaryRendered, "pushed to main")
}

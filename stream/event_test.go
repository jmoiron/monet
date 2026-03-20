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

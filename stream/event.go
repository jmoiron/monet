package stream

import (
	"fmt"
	"time"

	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/monarch"
)

var eventMigration = monarch.Set{
	Name: "event",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS event (
				id integer PRIMARY KEY,
				title text,
				source_id text,
				timestamp datetime,
				type text,
				url text,
				data text,
				summary_rendered text
			);`,
			Down: `DROP TABLE event;`,
		},
	},
}

// An Event is something like a post, a git commit, a photo upload, etc.
type Event struct {
	// An Id is a basic auto-increment id within the local db
	Id int
	// Title is a title for the event
	Title string
	// SourceId is the id of this event in the upstream system, eg. a commit hash
	// or a tweet ID
	SourceId string `db:"source_id"`
	// Timestamp is a unix timestamp of when this event happened
	Timestamp time.Time
	// Type is an indicator of where this came from, eg. "github" or "bluesky"
	Type string
	// Url is a permalink for this event
	Url string

	// Data is the full event in its original format, probably json
	Data string
	// SummaryRendered is a pre-rendered summary of the event, which gets displayed
	// on the event stream list
	SummaryRendered string `db:"summary_rendered"`
}

// An EventService manages events.
type EventService struct {
	db db.DB
}

func NewEventService(db db.DB) *EventService {
	return &EventService{db: db}
}

func (s *EventService) InsertArchive(e *Event) error {
	q := `INSERT INTO event
		(title, source_id, timestamp, type, url, data, summary_rendered) VALUES
		(:title, :source_id, :timestamp, :type, :url, :data, :summary_rendered);`

	stmt, err := s.db.PrepareNamed(q)
	if err != nil {
		return err
	}

	_, err = stmt.Exec(e)
	return err
}

// Select multiple posts via a query.
func (s *EventService) Select(where string, args ...interface{}) ([]*Event, error) {
	q := fmt.Sprintf(`SELECT * FROM event %s`, where)

	var events []*Event
	if err := s.db.Select(&events, q, args...); err != nil {
		return nil, err
	}

	return events, nil
}
package stream

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/monet/stream/sources"
	"github.com/jmoiron/sqlx"
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
		}, {
			Up: `CREATE VIRTUAL TABLE event_fts USING fts5(
				id, title, type, url, timestamp, data,
				content='event',
				content_rowid='id',
				tokenize="trigram"
			)`,
			Down: `drop table event_fts;`,
		}, {
			Up:   `INSERT INTO event_fts SELECT id, title, type, url, timestamp, data FROM event;`,
			Down: `DELETE FROM event_fts;`,
		}, {
			Up: `CREATE TRIGGER event_i AFTER INSERT ON event BEGIN
				INSERT INTO event_fts (id, title, type, url, timestamp, data) VALUES
					(new.id, new.title, new.type, new.url, new.timestamp, new.data);
			 END;`,
			Down: `DROP TRIGGER event_i;`,
		}, {
			Up: `CREATE TRIGGER event_d AFTER DELETE ON event BEGIN
				INSERT INTO event_fts (event_fts, id, title, type,  url, timestamp, data) VALUES
					('delete', old.id, old.title, old.type,  old.url, old.timestamp, old.data);
			END`,
			Down: `DROP TRIGGER event_d;`,
		}, {
			// delete + insert
			Up: `CREATE TRIGGER event_u AFTER UPDATE ON event BEGIN
				INSERT INTO event_fts (event_fts, id, title, type,  url, timestamp, data) VALUES
					('delete', old.id, old.title, old.type,  old.url, old.timestamp, old.data);
				INSERT INTO event_fts (id, title, type,  url, timestamp, data) VALUES
					(new.id, new.title, new.type,  new.url, new.timestamp, new.data);
			END`,
			Down: `DROP TRIGGER event_u;`,
		}, {
			Up: `DELETE FROM event
				WHERE source_id IS NOT NULL
				AND source_id <> ''
				AND id NOT IN (
					SELECT id FROM (
						SELECT id,
							row_number() OVER (
								PARTITION BY type, source_id
								ORDER BY length(COALESCE(data, '')) DESC, id DESC
							) AS rank
						FROM event
						WHERE source_id IS NOT NULL AND source_id <> ''
					)
					WHERE rank = 1
				);
				CREATE UNIQUE INDEX IF NOT EXISTS event_type_source_id
				ON event (type, source_id)
				WHERE source_id IS NOT NULL AND source_id <> '';`,
			Down: `DROP INDEX IF EXISTS event_type_source_id;`,
		}, {
			Up:   `ALTER TABLE event ADD COLUMN hidden integer NOT NULL DEFAULT 0;`,
			Down: `UPDATE event SET hidden=0;`,
		},
	},
}

// An Event is something like a post, a git commit, a photo upload, etc.
type Event struct {
	Id              int
	Title           string
	SourceId        string `db:"source_id"`
	Timestamp       time.Time
	Type            string
	Url             string
	Data            string
	SummaryRendered string `db:"summary_rendered"`
	Hidden          bool   `db:"hidden"`
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
		(title, source_id, timestamp, type, url, data, summary_rendered, hidden) VALUES
		(:title, :source_id, :timestamp, :type, :url, :data, :summary_rendered, :hidden);`

	stmt, err := s.db.PrepareNamed(q)
	if err != nil {
		return err
	}

	_, err = stmt.Exec(e)
	return err
}

// Upsert inserts or updates an event keyed by its source identifier and type.
func (s *EventService) Upsert(e *Event) error {
	if e.SourceId == "" {
		return s.InsertArchive(e)
	}

	return db.With(s.db, func(tx *sqlx.Tx) error {
		res, err := tx.Exec(`UPDATE event SET
			title=?,
			timestamp=?,
			url=?,
			data=?,
			summary_rendered=?,
			hidden=?
			WHERE type=? AND source_id=?`,
			e.Title,
			e.Timestamp,
			e.Url,
			e.Data,
			e.SummaryRendered,
			e.Hidden,
			e.Type,
			e.SourceId,
		)
		if err != nil {
			return err
		}

		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows > 0 {
			return nil
		}

		stmt, err := tx.PrepareNamed(`INSERT INTO event
			(title, source_id, timestamp, type, url, data, summary_rendered, hidden) VALUES
			(:title, :source_id, :timestamp, :type, :url, :data, :summary_rendered, :hidden)`)
		if err != nil {
			return err
		}

		if _, err := stmt.Exec(e); err != nil {
			// If another writer inserted the row after our update, retry as an update.
			if sqliteConflict(err) {
				_, err = tx.Exec(`UPDATE event SET
					title=?,
					timestamp=?,
					url=?,
					data=?,
					summary_rendered=?,
					hidden=?
					WHERE type=? AND source_id=?`,
					e.Title,
					e.Timestamp,
					e.Url,
					e.Data,
					e.SummaryRendered,
					e.Hidden,
					e.Type,
					e.SourceId,
				)
			}
			return err
		}
		return nil
	})
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

func (s *EventService) GetByID(id int) (*Event, error) {
	var event Event
	if err := s.db.Get(&event, `SELECT * FROM event WHERE id=?`, id); err != nil {
		return nil, err
	}
	return &event, nil
}

func (s *EventService) GetByTypeAndSourceID(eventType, sourceID string) (*Event, error) {
	var event Event
	if err := s.db.Get(&event, `SELECT * FROM event WHERE type=? AND source_id=?`, eventType, sourceID); err != nil {
		return nil, err
	}
	return &event, nil
}

func (s *EventService) CountByType(eventType string) (int, error) {
	var count int
	err := s.db.Get(&count, `SELECT count(*) FROM event WHERE type=?`, eventType)
	return count, err
}

func (s *EventService) RerenderByType(eventType string, settings map[string]string) (int, error) {
	events, err := s.Select("WHERE type=? ORDER BY id ASC", eventType)
	if err != nil {
		return 0, err
	}

	updated := 0
	for _, event := range events {
		evaluation, err := sources.Reevaluate(event.Type, event.Url, event.Data, settings)
		if err != nil {
			return updated, fmt.Errorf("rerender event %d: %w", event.Id, err)
		}
		if _, err := s.db.Exec(`UPDATE event SET summary_rendered=?, hidden=? WHERE id=?`, evaluation.SummaryRendered, evaluation.Hidden, event.Id); err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}

func (s *EventService) DeleteMissingByType(eventType string, keepSourceIDs []string) (int64, error) {
	if len(keepSourceIDs) == 0 {
		res, err := s.db.Exec(`DELETE FROM event WHERE type=?`, eventType)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keepSourceIDs)), ",")
	args := make([]any, 0, len(keepSourceIDs)+1)
	args = append(args, eventType)
	for _, id := range keepSourceIDs {
		args = append(args, id)
	}

	q := fmt.Sprintf(`DELETE FROM event WHERE type=? AND source_id NOT IN (%s)`, placeholders)
	res, err := s.db.Exec(q, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *EventService) LatestTimestampByType(eventType string) (time.Time, error) {
	var ts time.Time
	err := s.db.Get(&ts, `SELECT COALESCE(max(timestamp), 0) FROM event WHERE type=?`, eventType)
	return ts, err
}

func sqliteConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func PrettyEventData(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}

	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return raw
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		return raw
	}
	return strings.TrimRight(buf.String(), "\n")
}

package stream

import (
	"fmt"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
)

// event_fts is an external content table maintained only by triggers; any mix of
// inserts, updates and deletes must leave it consistent with the event table.
func TestEventFTSTriggersKeepIndexValid(t *testing.T) {
	scenarios := map[string][]string{
		"insert":                  {"i alpha", "i beta", "i gamma"},
		"update":                  {"i alpha", "i beta", "u 2 betax"},
		"update several times":    {"i alpha", "u 1 a1", "u 1 a2", "u 1 a3"},
		"delete first":            {"i alpha", "i beta", "i gamma", "d 1"},
		"delete last then insert": {"i alpha", "i beta", "d 2", "i delta"},
		"gaps in ids":             {"i alpha", "i beta", "i gamma", "d 2", "i delta", "u 3 gammax", "d 1"},
	}
	for name, steps := range scenarios {
		t.Run(name, func(t *testing.T) {
			db, err := sqlx.Connect("sqlite3", ":memory:")
			assert.NoError(t, err)
			db.SetMaxOpenConns(1)
			assert.NoError(t, NewApp(db).Migrate())

			for _, s := range steps {
				var word string
				var id int
				var err error
				switch s[0] {
				case 'i':
					fmt.Sscanf(s, "i %s", &word)
					_, err = db.Exec(`INSERT INTO event (title, source_id, type, url, data, timestamp)
						VALUES (?, ?, 'x', 'http://x/'||?, 'd', '2026-01-01')`, word+" title", word, word)
				case 'u':
					fmt.Sscanf(s, "u %d %s", &id, &word)
					_, err = db.Exec(`UPDATE event SET title=? WHERE id=?`, word+" title", id)
				case 'd':
					fmt.Sscanf(s, "d %d", &id)
					_, err = db.Exec(`DELETE FROM event WHERE id=?`, id)
				}
				assert.NoError(t, err, s)
			}

			_, err = db.Exec(`INSERT INTO event_fts(event_fts, rank) VALUES ('integrity-check', 1)`)
			assert.NoError(t, err, "index must be consistent with the event table")

			var found, live []int
			assert.NoError(t, db.Select(&found, `SELECT event.id FROM event_fts JOIN event ON event.id = event_fts.rowid
				WHERE event_fts MATCH 'title' ORDER BY event.id`))
			assert.NoError(t, db.Select(&live, `SELECT id FROM event ORDER BY id`))
			assert.Equal(t, live, found, "search finds exactly the live events")
		})
	}
}

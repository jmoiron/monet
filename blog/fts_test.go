package blog

import (
	"fmt"
	"testing"

	"github.com/jmoiron/monet/pkg/vfs"
	"github.com/jmoiron/monet/uploads"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
)

func newFTSTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Connect("sqlite3", ":memory:")
	assert.NoError(t, err)
	db.SetMaxOpenConns(1)
	fss := vfs.NewRegistry(vfs.NewURLMapper(nil))
	assert.NoError(t, uploads.NewApp(db, fss).Migrate())
	assert.NoError(t, NewApp(db, fss).Migrate())
	return db
}

// post_fts is an external content table maintained only by triggers; any mix of
// inserts, updates and deletes must leave it consistent with the post table.
func TestPostFTSTriggersKeepIndexValid(t *testing.T) {
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
			db := newFTSTestDB(t)
			for _, s := range steps {
				var word string
				var id int
				var err error
				switch s[0] {
				case 'i':
					fmt.Sscanf(s, "i %s", &word)
					_, err = db.Exec(`INSERT INTO post (title, slug, content, published) VALUES (?, ?, ?, 1)`, word+" title", word+"-slug", "body "+word)
				case 'u':
					fmt.Sscanf(s, "u %d %s", &id, &word)
					_, err = db.Exec(`UPDATE post SET title=?, slug=? WHERE id=?`, word+" title", word+"-slug", id)
				case 'd':
					fmt.Sscanf(s, "d %d", &id)
					_, err = db.Exec(`DELETE FROM post WHERE id=?`, id)
				}
				assert.NoError(t, err, s)
			}

			_, err := db.Exec(`INSERT INTO post_fts(post_fts, rank) VALUES ('integrity-check', 1)`)
			assert.NoError(t, err, "index must be consistent with the post table")

			var found, live []string
			assert.NoError(t, db.Select(&found, `SELECT slug FROM post_fts WHERE post_fts MATCH 'title' ORDER BY slug`))
			assert.NoError(t, db.Select(&live, `SELECT slug FROM post ORDER BY slug`))
			assert.Equal(t, live, found, "search finds exactly the live posts")
		})
	}
}

// Save used to rebuild the index after every write to hide the corruption.
func TestPostSaveUpdateKeepsSearchWorking(t *testing.T) {
	assert := assert.New(t)
	db := newFTSTestDB(t)
	serv := NewPostService(db)

	p := &Post{Title: "kangaroo facts", Content: "hop", Published: 1}
	assert.NoError(serv.Save(p))
	other := &Post{Title: "wombat facts", Content: "dig", Published: 1}
	assert.NoError(serv.Save(other))

	p.Title = "wallaby facts"
	assert.NoError(serv.Save(p))

	found, err := serv.Search("wallaby", QueryAll, 10, 0)
	assert.NoError(err)
	assert.Len(found, 1)
	found, err = serv.Search("kangaroo", QueryAll, 10, 0)
	assert.NoError(err)
	assert.Empty(found)
	found, err = serv.Search("wombat", QueryAll, 10, 0)
	assert.NoError(err)
	assert.Len(found, 1)

	_, err = db.Exec(`INSERT INTO post_fts(post_fts, rank) VALUES ('integrity-check', 1)`)
	assert.NoError(err)
}

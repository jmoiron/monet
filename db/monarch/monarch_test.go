package monarch

import (
	"database/sql"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
)

type countWrap struct {
	*sqlx.DB
	count int
}

func (c *countWrap) Get(dest interface{}, query string, args ...interface{}) error {
	c.count++
	return c.DB.Get(dest, query, args...)
}

func (c *countWrap) Exec(query string, args ...interface{}) (sql.Result, error) {
	c.count++
	return c.DB.Exec(query, args...)
}

func TestMonarch(t *testing.T) {
	assert := assert.New(t)

	db, err := sqlx.Connect("sqlite3", ":memory:")
	assert.NoError(err)
	assert.NotNil(db)
	defer db.Close()

	cw := &countWrap{DB: db}

	man, err := NewManager(cw)
	assert.NoError(err)
	assert.NotNil(man)
	// one bootstrap query, one version query, and then 4 queries;
	// m.Up + SetVersion for each migration query
	assert.Equal(cw.count, 6)

	cw.count = 0

	version, err := man.GetVersion("monarch")
	assert.NoError(err)
	assert.Equal(version, len(man.bootstrapMigrations())-1)

	assert.Equal(cw.count, 1)

	cw.count = 0

	man, err = NewManager(cw)
	assert.NoError(err)
	assert.NotNil(man)

	// one bootstrap query, one version query but that's it
	assert.Equal(cw.count, 2)

}

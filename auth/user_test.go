package auth

import (
	"testing"

	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
)

func Ensure(db db.DB) error {
	// create all tables if they don't exist
	user := `CREATE TABLE IF NOT EXISTS user (
		id INTEGER PRIMARY KEY,
		username TEXT,
		password_hash TEXT
	);`

	tables := []string{user}
	indexes := []string{}

	var ex []string
	ex = append(ex, tables...)
	ex = append(ex, indexes...)

	for _, stmt := range ex {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}

	return nil
}

func TestPost(t *testing.T) {
	assert := assert.New(t)

	conn, err := sqlx.Connect("sqlite3", ":memory:")
	assert.NoError(err)

	assert.NoError(Ensure(conn))

	_, err = conn.Exec(`SELECT * FROM user`)
	assert.NoError(err)

	serv := NewService(conn)

	err = serv.CreateUser("shayla shayla", "weak")
	assert.NoError(err)

	// existing user should validate
	ok, err := serv.Validate("shayla shayla", "weak")
	assert.True(ok)
	assert.NoError(err)

	ok, err = serv.Validate("shayla shayla", "stronk")
	assert.False(ok, "wrong pw validates")
	ok, err = serv.Validate("vegetables", "weak")
	assert.False(ok, "wrong user validates")

	ok, err = serv.ChangePassword("shayla shayla", "weak", "stronk")
	assert.True(ok)
	assert.NoError(err)

	ok, err = serv.Validate("shayla shayla", "stronk")
	assert.True(ok)

	// password check failure
	ok, err = serv.ChangePassword("shayla shayla", "weak", "best")
	assert.False(ok)
}

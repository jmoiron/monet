package auth

import (
	"testing"

	"github.com/jmoiron/monet/conf"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
)

func TestPost(t *testing.T) {
	assert := assert.New(t)

	conn, err := sqlx.Connect("sqlite3", ":memory:")
	assert.NoError(err)

	app := NewApp(conf.Default(), conn)
	assert.NoError(app.Migrate())

	_, err = conn.Exec(`SELECT * FROM user;`)
	assert.NoError(err)

	serv := NewUserService(conn)

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

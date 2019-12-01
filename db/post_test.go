package db

import (
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
)

func TestPost(t *testing.T) {
	assert := assert.New(t)

	db, err := sqlx.Connect("sqlite3", ":memory:")
	assert.NoError(err)

	assert.NoError(Ensure(db))

	p := &Post{
		Title:     "hello, world",
		Content:   "hi",
		Published: 0,
		Tags:      []string{"first", "post"},
	}

	err = p.Save(db)
	assert.NoError(err)

	assert.True(p.ID > 0)

	var p2 Post
	assert.NoError(db.Get(&p2, `SELECT * FROM post WHERE id=?`, p.ID))

	assert.Equal(p.ID, p2.ID)
	assert.Equal(p.Title, p2.Title)
	// we didn't load tags but we should have saved them..
	assert.False(len(p.Tags) == len(p2.Tags))

	serv := NewPostService(db)
	p3, err := serv.Get(int(p.ID))
	assert.NoError(err)
	assert.NotNil(p3)

	assert.Equal(p3.ID, p.ID)
	assert.Equal(p3.Title, p.Title)
	// this time we loaded tags..  they should be the same
	assert.ElementsMatch(p.Tags, p3.Tags)

	p3.Title = "nevermind planet!"
	p3.Published = 2
	p3.Tags = []string{"last", "post"}
	assert.NoError(p3.Save(db))

	// should update existing, not change any
	var count int
	assert.NoError(db.Get(&count, `SELECT count(*) FROM post;`))
	assert.Equal(1, count)

	p4, err := serv.Get(int(p.ID))
	assert.NoError(err)
	assert.NotNil(p4)
	assert.ElementsMatch(p3.Tags, p4.Tags)

}

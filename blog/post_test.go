package blog

import (
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
)

func TestPost(t *testing.T) {
	assert := assert.New(t)

	db, err := sqlx.Connect("sqlite3", ":memory:")
	assert.NoError(err)

	assert.NoError(NewApp(db).Migrate())

	serv := NewPostService(db)

	p := &Post{
		Title:     "hello, world",
		Content:   "hi",
		Published: 0,
		Tags:      []string{"first", "post"},
	}

	err = serv.Save(p)
	assert.NoError(err)

	assert.True(p.ID > 0)

	var p2 Post
	assert.NoError(db.Get(&p2, `SELECT * FROM post WHERE id=?`, p.ID))

	assert.Equal(p.ID, p2.ID)
	assert.Equal(p.Title, p2.Title)
	// we didn't load tags but we should have saved them..
	assert.False(len(p.Tags) == len(p2.Tags))

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
	p3.now = func() time.Time {
		return p3.UpdatedAt.Add(time.Minute)
	}
	assert.NoError(serv.Save(p3))

	// should update existing, not change any
	var count int
	assert.NoError(db.Get(&count, `SELECT count(*) FROM post;`))
	assert.Equal(1, count)

	p4, err := serv.Get(int(p.ID))
	assert.NoError(err)
	assert.NotNil(p4)
	assert.ElementsMatch(p3.Tags, p4.Tags)

	// p3.UpdatedAt should have been set automatically on save
	assert.Equal(p4.UpdatedAt, p3.UpdatedAt)
	// and it should be different in p4 (the db) vs the previous value
	assert.NotEqual(p4.UpdatedAt, p2.UpdatedAt)

}

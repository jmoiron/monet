package blog

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/jmoiron/monet/db"
)

// load.go contains routines for loading data from json into the db

type Loader struct {
	db db.DB
}

type jsonPost struct {
	ID              json.RawMessage `json:"_id"`
	Title           string
	Slug            string
	Content         string
	ContentRendered string
	Summary         string
	Tags            []string
	Timestamp       int
	Published       int
}

func NewLoader(db db.DB) *Loader {
	return &Loader{db}
}

func (l *Loader) Load(r io.Reader) error {
	decoder := json.NewDecoder(r)
	serv := NewPostService(l.db)

	for {
		var jp jsonPost
		err := decoder.Decode(&jp)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("decode: %w", err)
		}

		p := Post{
			Content:         jp.Content,
			ContentRendered: jp.ContentRendered,
			Title:           jp.Title,
			Slug:            jp.Slug,
			CreatedAt:       time.Unix(int64(jp.Timestamp), 0),
			UpdatedAt:       time.Unix(int64(jp.Timestamp), 0),
			Published:       jp.Published,
		}

		if p.Published > 0 {
			p.PublishedAt = p.CreatedAt
		}

		if err := serv.InsertArchive(&p); err != nil {
			fmt.Printf("%#v\n", jp)
			return fmt.Errorf("save: %w", err)
		}

	}
}

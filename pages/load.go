package pages

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/jmoiron/monet/db"
)

type Loader struct {
	db db.DB
}

type jsonPage struct {
	Id              json.RawMessage `json:"_id"`
	URL             string
	Content         string
	ContentRendered string `json:"content_rendered"`
}

func NewLoader(db db.DB) *Loader {
	return &Loader{db: db}
}

func (l *Loader) Load(r io.Reader) error {
	decoder := json.NewDecoder(r)
	serv := NewPageService(l.db)

	for {
		var jp jsonPage
		err := decoder.Decode(&jp)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("decode: %w", err)
		}

		p := Page{
			URL:             jp.URL,
			Content:         jp.Content,
			ContentRendered: jp.ContentRendered,
		}

		if err := serv.InsertArchive(&p); err != nil {
			fmt.Printf("%#v\n", jp)
			return fmt.Errorf("save: %w", err)
		}
	}
}

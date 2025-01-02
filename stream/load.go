package stream

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/jmoiron/monet/db"
)

type Loader struct {
	db db.DB
}

type jsonEvent struct {
	Id              json.RawMessage `json:"_id"`
	Title           string
	SourceId        string
	Url             string
	Type            string
	Data            string
	SummaryRendered string
	Timestamp       int64
}

func NewLoader(db db.DB) *Loader {
	return &Loader{db: db}
}

func (l *Loader) Load(r io.Reader) error {
	decoder := json.NewDecoder(r)
	serv := NewEventService(l.db)

	for {
		var je jsonEvent
		err := decoder.Decode(&je)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("decode: %w", err)
		}

		e := Event{
			Title:           je.Title,
			SourceId:        je.SourceId,
			Timestamp:       time.Unix(je.Timestamp, 0),
			SummaryRendered: je.SummaryRendered,
			Type:            je.Type,
			Url:             je.Url,
			Data:            je.Data,
		}

		if err := serv.InsertArchive(&e); err != nil {
			fmt.Printf("%#v\n", je)
			return fmt.Errorf("save: %w", err)
		}
	}
}

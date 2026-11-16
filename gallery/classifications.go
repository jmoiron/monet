package gallery

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/pkg/classify"
	"github.com/jmoiron/sqlx"
)

// A Classification is what one model saw in one photo.
type Classification struct {
	PhotoID   string `db:"photo_id"`
	Model     string `db:"model"`
	Caption   string `db:"caption"`
	LabelsRaw string `db:"labels"`
	Embedding []byte `db:"embedding"` // float16s; nil when the model gave none
	CreatedAt int64  `db:"created_at"`
}

// Labels are the classification's labels.
func (c *Classification) Labels() []classify.Label {
	var l []classify.Label
	json.Unmarshal([]byte(c.LabelsRaw), &l)
	return l
}

// LabelNames are the labels as a list of words, for showing.
func (c *Classification) LabelNames() []string {
	var names []string
	for _, l := range c.Labels() {
		names = append(names, l.Name)
	}
	return names
}

// HasEmbedding is whether the model gave an embedding.
func (c *Classification) HasEmbedding() bool { return len(c.Embedding) > 0 }

// A ModelCount is how many photos a model has classified.
type ModelCount struct {
	Model      string `db:"model"`
	Photos     int    `db:"photos"`
	Embeddings int    `db:"embeddings"`
}

// ClassService keeps what models saw in photos.
type ClassService struct{ db db.DB }

func NewClassService(d db.DB) *ClassService { return &ClassService{db: d} }

// For lists what the models saw in a photo, the model names in order.
func (s *ClassService) For(photoID string) ([]Classification, error) {
	var cs []Classification
	err := s.db.Select(&cs, `SELECT photo_id, model, caption, labels, embedding, created_at
		FROM photo_class WHERE photo_id=? ORDER BY model`, photoID)
	return cs, err
}

// Models lists the models there are classifications from.
func (s *ClassService) Models() ([]ModelCount, error) {
	var ms []ModelCount
	err := s.db.Select(&ms, `SELECT model, count(*) AS photos,
		count(embedding) AS embeddings FROM photo_class GROUP BY model ORDER BY model`)
	return ms, err
}

// An ClassifyReport says what an Import did with its items.
type ClassifyReport struct {
	Stored   int `json:"stored"`
	Unknown  int `json:"unknown"`  // for a photo this gallery doesn't have
	Mismatch int `json:"mismatch"` // for another image than the photo's (its sha256 differs)
	Invalid  int `json:"invalid"`  // not an item at all
}

func (r *ClassifyReport) add(o ClassifyReport) {
	r.Stored += o.Stored
	r.Unknown += o.Unknown
	r.Mismatch += o.Mismatch
	r.Invalid += o.Invalid
}

// Import stores the items of an export, replacing what the same model said of the
// same photo before. Items for photos that are not here, or not the image they
// were made from, are counted and left out.
func (s *ClassService) Import(items []classify.Item) (ClassifyReport, error) {
	var rep ClassifyReport
	err := db.With(s.db, func(tx *sqlx.Tx) error {
		for _, it := range items {
			if it.ID == "" || it.Model == "" || strings.TrimSpace(it.Caption) == "" && len(it.Labels) == 0 && it.Embedding == "" {
				rep.Invalid++
				continue
			}
			var sha string
			if err := tx.Get(&sha, `SELECT sha256 FROM photo WHERE id=?`, it.ID); err != nil {
				rep.Unknown++
				continue
			}
			if it.SHA256 != "" && it.SHA256 != sha {
				rep.Mismatch++
				continue
			}
			var emb []byte
			if it.Embedding != "" {
				v, err := classify.DecodeEmbedding(it.Embedding)
				if err != nil || len(v) == 0 {
					rep.Invalid++
					continue
				}
				emb = halves(it.Embedding)
			}
			labels, _ := json.Marshal(it.Labels)
			if it.Labels == nil {
				labels = []byte("[]")
			}
			if _, err := tx.Exec(`INSERT OR REPLACE INTO photo_class (photo_id, model, caption, labels, embedding)
				VALUES (?, ?, ?, ?, ?)`, it.ID, it.Model, strings.TrimSpace(it.Caption), string(labels), emb); err != nil {
				return err
			}
			if err := reindex(tx, it.ID); err != nil {
				return err
			}
			rep.Stored++
		}
		return nil
	})
	if err != nil {
		return ClassifyReport{}, fmt.Errorf("storing classifications: %w", err)
	}
	return rep, nil
}

// halves is the bytes of an encoded embedding (already checked to decode).
func halves(enc string) []byte {
	b, _ := base64.StdEncoding.DecodeString(enc)
	return b
}

// reindex makes the rows of a photo in photo_class_fts what it is now: a row for
// each model, with the words of the photo's place as well, so that a search
// for "tokyo sunset" finds a caption with the one and a place with the other;
// and a row of the place alone, so that a photo that no model has seen is
// found by where it was taken.
func reindex(tx *sqlx.Tx, id string) error {
	if _, err := tx.Exec(`DELETE FROM photo_class_fts WHERE photo_id=?`, id); err != nil {
		return err
	}
	var pl Place
	var place string
	if err := tx.Get(&pl, `SELECT * FROM photo_place WHERE photo_id=?`, id); err == nil {
		place = pl.Words()
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var rows []struct {
		Model   string `db:"model"`
		Caption string `db:"caption"`
		Labels  string `db:"labels"`
	}
	if err := tx.Select(&rows, `SELECT model, caption, labels FROM photo_class WHERE photo_id=?`, id); err != nil {
		return err
	}
	for _, r := range rows {
		c := Classification{LabelsRaw: r.Labels}
		if _, err := tx.Exec(`INSERT INTO photo_class_fts (photo_id, model, caption, labels, place) VALUES (?, ?, ?, ?, ?)`,
			id, r.Model, r.Caption, strings.Join(c.LabelNames(), "\n"), place); err != nil {
			return err
		}
	}
	if place != "" {
		if _, err := tx.Exec(`INSERT INTO photo_class_fts (photo_id, model, caption, labels, place) VALUES (?, 'place', '', '', ?)`, id, place); err != nil {
			return err
		}
	}
	return nil
}

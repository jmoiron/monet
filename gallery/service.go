package gallery

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/sqlx"
)

// ErrNotAnImage is returned for data that isn't an image we can read.
var ErrNotAnImage = errors.New("not an image")

// A PhotoService keeps photos: rows in the database, files in the store.
type PhotoService struct {
	db    db.DB
	store *Store
}

func NewPhotoService(db db.DB, store *Store) *PhotoService {
	return &PhotoService{db: db, store: store}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// namedExec runs a query with named parameters taken from arg.
func namedExec(d db.DB, query string, arg any) (sql.Result, error) {
	var res sql.Result
	err := db.With(d, func(tx *sqlx.Tx) (err error) {
		res, err = tx.NamedExec(query, arg)
		return err
	})
	return res, err
}

// Store is where the service keeps its files.
func (s *PhotoService) Store() *Store { return s.store }

// NewPhoto is a photo to be added. Data is the image file; the rest is what
// the source knows that the file may not say. The file's own metadata wins
// over TakenAt and the coordinates, which only fill what it leaves out.
type NewPhoto struct {
	// ID is optional, and is how a source that wants stable ids gets them.
	ID          string
	Source      string
	SourceID    string
	SourceGroup string

	Data []byte

	Title, Caption      string
	Software            string
	PostedAt, TakenAt   int64
	Latitude, Longitude *float64
	Likes, Comments     int
	Meta                Meta

	Published bool
}

// Add stores the image and a photo for it.
func (s *PhotoService) Add(n NewPhoto) (*Photo, error) {
	if n.Source == "" {
		return nil, errors.New("a photo needs a source")
	}
	ex := ReadExif(n.Data)
	img, err := LookAt(n.Data, ex.Orientation)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotAnImage, err)
	}

	id := n.ID
	if id == "" {
		id = uuid.NewString()
	}

	p := &Photo{
		ID:          id,
		Source:      n.Source,
		SourceID:    n.SourceID,
		SourceGroup: n.SourceGroup,
		Mime:        img.Mime,
		Bytes:       int64(len(n.Data)),
		Width:       img.Width,
		Height:      img.Height,
		SHA256:      sha256Hex(n.Data),
		Title:       n.Title,
		Caption:     n.Caption,
		PostedAt:    n.PostedAt,
		CameraMake:  ex.Make,
		CameraModel: ex.Model,
		LensModel:   ex.Lens,
		FocalLength: ex.FocalLength,
		Aperture:    ex.Aperture,
		ISO:         ex.ISO,
		Exposure:    ex.Exposure,
		Software:    ex.Software,
		Latitude:    ex.Latitude,
		Longitude:   ex.Longitude,
		Altitude:    ex.Altitude,
		Likes:       n.Likes,
		Comments:    n.Comments,
	}
	switch {
	case !ex.Taken.IsZero():
		p.TakenAt = ex.Taken.Unix()
	case n.TakenAt > 0:
		p.TakenAt = n.TakenAt
	default:
		p.TakenAt = n.PostedAt
	}
	if p.Software == "" {
		p.Software = n.Software
	}
	if p.Latitude == nil || p.Longitude == nil {
		p.Latitude, p.Longitude = n.Latitude, n.Longitude
	}
	if n.Published {
		p.Published = 1
	}
	if err := p.setMeta(n.Meta); err != nil {
		return nil, err
	}

	shard := id[:2]
	p.File = fmt.Sprintf("orig/%s/%s/%s%s", n.Source, shard, id, img.Ext)
	files := map[string][]byte{p.File: n.Data}
	if img.Large != nil {
		p.Large = fmt.Sprintf("large/%s/%s.jpg", shard, id)
		files[p.Large] = img.Large
	}
	if img.Thumb != nil {
		p.Thumb = fmt.Sprintf("thumb/%s/%s.jpg", shard, id)
		files[p.Thumb] = img.Thumb
	}
	for rel, data := range files {
		if err := s.store.Write(rel, data); err != nil {
			s.store.Remove(p.File, p.Large, p.Thumb)
			return nil, fmt.Errorf("storing %s: %w", rel, err)
		}
	}

	now := time.Now().Unix()
	p.CreatedAt, p.UpdatedAt = now, now
	if err := s.insert(p); err != nil {
		s.store.Remove(p.File, p.Large, p.Thumb)
		return nil, err
	}
	return p, nil
}

const photoColumns = `id, source, source_id, source_group, file, large, thumb, mime, bytes, width, height, sha256,
	title, caption, taken_at, posted_at, camera_make, camera_model, lens_model, focal_length, aperture, iso,
	exposure, software, latitude, longitude, altitude, likes, comments, meta, published, created_at, updated_at`

func (s *PhotoService) insert(p *Photo) error {
	named := strings.Builder{}
	for i, c := range strings.Split(photoColumns, ",") {
		if i > 0 {
			named.WriteString(", ")
		}
		named.WriteString(":" + strings.TrimSpace(c))
	}
	return db.With(s.db, func(tx *sqlx.Tx) error {
		_, err := tx.NamedExec(`INSERT INTO photo (`+photoColumns+`) VALUES (`+named.String()+`)`, p)
		return err
	})
}

// Save writes back what can change about a photo: its words, its coordinates,
// the counts and whether it's shown. Its files and identity can't.
func (s *PhotoService) Save(p *Photo) error {
	p.UpdatedAt = time.Now().Unix()
	_, err := namedExec(s.db, `UPDATE photo SET
		title=:title, caption=:caption, taken_at=:taken_at, posted_at=:posted_at,
		latitude=:latitude, longitude=:longitude, altitude=:altitude,
		likes=:likes, comments=:comments, meta=:meta, source_group=:source_group,
		published=:published, updated_at=:updated_at
	WHERE id=:id`, p)
	return err
}

// Get returns the photo with id.
func (s *PhotoService) Get(id string) (*Photo, error) {
	var p Photo
	if err := s.db.Get(&p, `SELECT `+photoColumns+` FROM photo WHERE id=?`, id); err != nil {
		return nil, err
	}
	return &p, nil
}

// Find returns the photo that has this id at its source, or nil if there isn't one.
func (s *PhotoService) Find(source, sourceID string) (*Photo, error) {
	var p Photo
	err := s.db.Get(&p, `SELECT `+photoColumns+` FROM photo WHERE source=? AND source_id=? AND source_id != ''`, source, sourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// FindBySHA returns a photo from source with exactly this file, or nil.
func (s *PhotoService) FindBySHA(source, sha string) (*Photo, error) {
	var p Photo
	err := s.db.Get(&p, `SELECT `+photoColumns+` FROM photo WHERE source=? AND sha256=? LIMIT 1`, source, sha)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// OrderNewest sorts photos from newest to oldest, by when they were taken, or
// when they were posted if that isn't known, or failing that when they were added.
// (Uploads without a date in them would otherwise sort as if from 1970.)
const OrderNewest = `ORDER BY ` + sortTime + ` DESC, id`

// Select returns the photos matching a query tail like "WHERE published > 0 "+OrderNewest.
func (s *PhotoService) Select(query string, args ...any) ([]Photo, error) {
	var photos []Photo
	if err := s.db.Select(&photos, `SELECT `+photoColumns+` FROM photo `+query, args...); err != nil {
		return nil, err
	}
	return photos, nil
}

// Count counts the photos matching a where clause (or all of them if it is "").
func (s *PhotoService) Count(where string, args ...any) (int, error) {
	var n int
	err := s.db.Get(&n, `SELECT count(*) FROM photo `+where, args...)
	return n, err
}

// Delete removes the photo and its files.
func (s *PhotoService) Delete(id string) error {
	p, err := s.Get(id)
	if err != nil {
		return err
	}
	err = db.With(s.db, func(tx *sqlx.Tx) error {
		if _, err := tx.Exec(`DELETE FROM album_photo WHERE photo_id=?`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE album SET cover_id='' WHERE cover_id=?`, id); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM photo WHERE id=?`, id)
		return err
	})
	if err != nil {
		return err
	}
	return s.store.Remove(p.File, p.Large, p.Thumb)
}

// DisplayFile is the file a page should show: the web sized copy if there is one.
func (p *Photo) DisplayFile() string {
	if p.Large != "" {
		return p.Large
	}
	return p.File
}

// ThumbFile is the thumbnail, or the best thing there is if it couldn't be made.
func (p *Photo) ThumbFile() string {
	if p.Thumb != "" {
		return p.Thumb
	}
	return p.DisplayFile()
}

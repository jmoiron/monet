package gallery

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/sqlx"
)

// AlbumService keeps albums, which are named, ordered groups of photos. A
// photo can be in any number of them.
type AlbumService struct {
	db db.DB
}

func NewAlbumService(db db.DB) *AlbumService { return &AlbumService{db: db} }

const albumColumns = `id, slug, title, description, cover_id, published, created_at, updated_at`

// Create makes a new album. The slug is made from the title, and made unique.
func (s *AlbumService) Create(title, description string) (*Album, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, errors.New("an album needs a title")
	}
	base := db.Slugify(title)
	if base == "" {
		base = "album"
	}
	slug := base
	for n := 2; ; n++ {
		var exists int
		if err := s.db.Get(&exists, `SELECT count(*) FROM album WHERE slug=?`, slug); err != nil {
			return nil, err
		}
		if exists == 0 {
			break
		}
		slug = fmt.Sprintf("%s-%d", base, n)
	}

	now := time.Now().Unix()
	a := &Album{ID: uuid.NewString(), Slug: slug, Title: title, Description: description, CreatedAt: now, UpdatedAt: now}
	_, err := namedExec(s.db, `INSERT INTO album (`+albumColumns+`) VALUES
		(:id, :slug, :title, :description, :cover_id, :published, :created_at, :updated_at)`, a)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// Get returns the album with id.
func (s *AlbumService) Get(id string) (*Album, error) {
	var a Album
	if err := s.db.Get(&a, `SELECT `+albumColumns+` FROM album WHERE id=?`, id); err != nil {
		return nil, err
	}
	return &a, nil
}

// BySlug returns the album with this slug.
func (s *AlbumService) BySlug(slug string) (*Album, error) {
	var a Album
	if err := s.db.Get(&a, `SELECT `+albumColumns+` FROM album WHERE slug=?`, slug); err != nil {
		return nil, err
	}
	return &a, nil
}

// List returns the albums, newest first; only published ones if publishedOnly.
func (s *AlbumService) List(publishedOnly bool) ([]Album, error) {
	q := `SELECT ` + albumColumns + ` FROM album `
	if publishedOnly {
		q += `WHERE published > 0 `
	}
	var albums []Album
	err := s.db.Select(&albums, q+`ORDER BY created_at DESC`)
	return albums, err
}

// Save writes back an album's title, description, cover and whether it's shown.
func (s *AlbumService) Save(a *Album) error {
	a.UpdatedAt = time.Now().Unix()
	_, err := namedExec(s.db, `UPDATE album SET title=:title, description=:description,
		cover_id=:cover_id, published=:published, updated_at=:updated_at WHERE id=:id`, a)
	return err
}

// Delete removes the album, but not its photos.
func (s *AlbumService) Delete(id string) error {
	return db.With(s.db, func(tx *sqlx.Tx) error {
		if _, err := tx.Exec(`DELETE FROM album_photo WHERE album_id=?`, id); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM album WHERE id=?`, id)
		return err
	})
}

// Add puts photos at the end of an album, skipping those already in it.
func (s *AlbumService) Add(albumID string, photoIDs ...string) error {
	return db.With(s.db, func(tx *sqlx.Tx) error {
		var next int
		if err := tx.Get(&next, `SELECT coalesce(max(position), 0) + 1 FROM album_photo WHERE album_id=?`, albumID); err != nil {
			return err
		}
		for _, id := range photoIDs {
			res, err := tx.Exec(`INSERT OR IGNORE INTO album_photo (album_id, photo_id, position)
				SELECT ?, id, ? FROM photo WHERE id=?`, albumID, next, id)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				next++
			}
		}
		return nil
	})
}

// Remove takes photos out of an album.
func (s *AlbumService) Remove(albumID string, photoIDs ...string) error {
	return db.With(s.db, func(tx *sqlx.Tx) error {
		for _, id := range photoIDs {
			if _, err := tx.Exec(`DELETE FROM album_photo WHERE album_id=? AND photo_id=?`, albumID, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// Photos returns the photos of an album in its order; only published ones if
// publishedOnly.
func (s *AlbumService) Photos(albumID string, publishedOnly bool) ([]Photo, error) {
	q := `SELECT ` + qualified("p", photoColumns) + ` FROM photo p
		JOIN album_photo ap ON ap.photo_id = p.id WHERE ap.album_id=? `
	if publishedOnly {
		q += `AND p.published > 0 `
	}
	var photos []Photo
	err := s.db.Select(&photos, q+`ORDER BY ap.position, p.taken_at`, albumID)
	return photos, err
}

// AlbumsOf returns the albums a photo is in; only published ones if publishedOnly.
func (s *AlbumService) AlbumsOf(photoID string, publishedOnly bool) ([]Album, error) {
	q := `SELECT ` + qualified("a", albumColumns) + ` FROM album a
		JOIN album_photo ap ON ap.album_id = a.id WHERE ap.photo_id=? `
	if publishedOnly {
		q += `AND a.published > 0 `
	}
	var albums []Album
	err := s.db.Select(&albums, q+`ORDER BY a.title`, photoID)
	return albums, err
}

func qualified(alias, columns string) string {
	parts := strings.Split(columns, ",")
	for i, c := range parts {
		parts[i] = alias + "." + strings.TrimSpace(c)
	}
	return strings.Join(parts, ", ")
}

// An AlbumSummary is an album with how many photos it has, and a photo to
// stand for it.
type AlbumSummary struct {
	Album
	Count      int    `db:"photo_count"`
	CoverFile  string `db:"cover_file"`
	CoverThumb string `db:"cover_thumb"`
}

// Cover is the file to show for the album: its first photo.
func (a *AlbumSummary) Cover() string {
	if a.CoverThumb != "" {
		return a.CoverThumb
	}
	return a.CoverFile
}

// Summaries lists the albums, newest first, with their sizes. Only published
// albums, and only published photos in them, if publishedOnly.
func (s *AlbumService) Summaries(publishedOnly bool) ([]AlbumSummary, error) {
	visible, where := "", ""
	if publishedOnly {
		visible = ` AND p.published > 0`
		where = `WHERE a.published > 0 `
	}
	var out []AlbumSummary
	err := s.db.Select(&out, `SELECT `+qualified("a", albumColumns)+`,
		(SELECT count(*) FROM album_photo ap JOIN photo p ON p.id = ap.photo_id
			WHERE ap.album_id = a.id`+visible+`) AS photo_count,
		coalesce((SELECT p.file FROM album_photo ap JOIN photo p ON p.id = ap.photo_id
			WHERE ap.album_id = a.id`+visible+` ORDER BY ap.position, p.id LIMIT 1), '') AS cover_file,
		coalesce((SELECT p.thumb FROM album_photo ap JOIN photo p ON p.id = ap.photo_id
			WHERE ap.album_id = a.id`+visible+` ORDER BY ap.position, p.id LIMIT 1), '') AS cover_thumb
		FROM album a `+where+`ORDER BY a.created_at DESC, a.title`)
	return out, err
}

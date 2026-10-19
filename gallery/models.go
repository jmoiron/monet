package gallery

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/monet/db/monarch"
)

// Sources a photo can come from.
const (
	SourceUpload    = "upload"
	SourceInstagram = "instagram"
	SourceGPhotos   = "gphotos"
)

var galleryMigrations = monarch.Set{
	Name: "gallery",
	Migrations: []monarch.Migration{
		{
			// A photo is one image, wherever it came from. The file itself (and its
			// thumbnails) live in the "photos" filesystem, not in the database; the
			// paths here are relative to it.
			//
			// (source, source_id) identifies a photo at its source, which is what
			// makes an import idempotent: instagram's media id, google's item id, or
			// for uploads nothing at all. Times are unix seconds; taken_at is the
			// camera's wall clock as if it were UTC, as there is rarely a zone.
			Up: `CREATE TABLE IF NOT EXISTS photo (
				id text PRIMARY KEY,
				source text NOT NULL,
				source_id text NOT NULL DEFAULT '',
				source_group text NOT NULL DEFAULT '',

				file text NOT NULL,
				large text NOT NULL DEFAULT '',
				thumb text NOT NULL DEFAULT '',
				mime text NOT NULL DEFAULT '',
				bytes integer NOT NULL DEFAULT 0,
				width integer NOT NULL DEFAULT 0,
				height integer NOT NULL DEFAULT 0,
				sha256 text NOT NULL DEFAULT '',

				title text NOT NULL DEFAULT '',
				caption text NOT NULL DEFAULT '',
				taken_at integer NOT NULL DEFAULT 0,
				posted_at integer NOT NULL DEFAULT 0,

				camera_make text NOT NULL DEFAULT '',
				camera_model text NOT NULL DEFAULT '',
				lens_model text NOT NULL DEFAULT '',
				focal_length real NOT NULL DEFAULT 0,
				aperture real NOT NULL DEFAULT 0,
				iso integer NOT NULL DEFAULT 0,
				exposure text NOT NULL DEFAULT '',
				software text NOT NULL DEFAULT '',
				latitude real,
				longitude real,
				altitude real,

				likes integer NOT NULL DEFAULT 0,
				comments integer NOT NULL DEFAULT 0,
				meta text NOT NULL DEFAULT '{}',

				published integer NOT NULL DEFAULT 0,
				created_at integer NOT NULL DEFAULT (strftime('%s', 'now')),
				updated_at integer NOT NULL DEFAULT (strftime('%s', 'now'))
			);`,
			Down: `DROP TABLE photo;`,
		},
		{
			Up:   `CREATE UNIQUE INDEX IF NOT EXISTS idx_photo_source ON photo(source, source_id) WHERE source_id != '';`,
			Down: `DROP INDEX idx_photo_source;`,
		},
		{
			Up:   `CREATE INDEX IF NOT EXISTS idx_photo_taken ON photo(published, taken_at);`,
			Down: `DROP INDEX idx_photo_taken;`,
		},
		{
			Up:   `CREATE INDEX IF NOT EXISTS idx_photo_sha256 ON photo(sha256);`,
			Down: `DROP INDEX idx_photo_sha256;`,
		},
		{
			Up: `CREATE TABLE IF NOT EXISTS album (
				id text PRIMARY KEY,
				slug text NOT NULL UNIQUE,
				title text NOT NULL,
				description text NOT NULL DEFAULT '',
				cover_id text NOT NULL DEFAULT '',
				published integer NOT NULL DEFAULT 0,
				created_at integer NOT NULL DEFAULT (strftime('%s', 'now')),
				updated_at integer NOT NULL DEFAULT (strftime('%s', 'now'))
			);`,
			Down: `DROP TABLE album;`,
		},
		{
			Up: `CREATE TABLE IF NOT EXISTS album_photo (
				album_id text NOT NULL REFERENCES album(id) ON DELETE CASCADE,
				photo_id text NOT NULL REFERENCES photo(id) ON DELETE CASCADE,
				position integer NOT NULL DEFAULT 0,
				PRIMARY KEY (album_id, photo_id)
			);`,
			Down: `DROP TABLE album_photo;`,
		},
		{
			Up:   `CREATE INDEX IF NOT EXISTS idx_album_photo_photo ON album_photo(photo_id);`,
			Down: `DROP INDEX idx_album_photo_photo;`,
		},
	},
}

// A Photo is an image and what is known about it.
type Photo struct {
	ID          string `db:"id"`
	Source      string `db:"source"`
	SourceID    string `db:"source_id"`
	SourceGroup string `db:"source_group"` // photos of one post (a carousel) share this

	File   string `db:"file"`  // the original, relative to the photos filesystem
	Large  string `db:"large"` // a web sized copy (the original when it is small enough)
	Thumb  string `db:"thumb"`
	Mime   string `db:"mime"`
	Bytes  int64  `db:"bytes"`
	Width  int    `db:"width"` // as displayed: the camera's orientation is applied
	Height int    `db:"height"`
	SHA256 string `db:"sha256"`

	Title    string `db:"title"`
	Caption  string `db:"caption"`
	TakenAt  int64  `db:"taken_at"`  // wall clock, as if UTC; 0 when unknown
	PostedAt int64  `db:"posted_at"` // when it was published at its source

	CameraMake  string   `db:"camera_make"`
	CameraModel string   `db:"camera_model"`
	LensModel   string   `db:"lens_model"`
	FocalLength float64  `db:"focal_length"` // mm
	Aperture    float64  `db:"aperture"`     // f-number
	ISO         int      `db:"iso"`
	Exposure    string   `db:"exposure"` // eg. "1/250"
	Software    string   `db:"software"`
	Latitude    *float64 `db:"latitude"`
	Longitude   *float64 `db:"longitude"`
	Altitude    *float64 `db:"altitude"`

	Likes    int    `db:"likes"`
	Comments int    `db:"comments"`
	MetaJSON string `db:"meta"`

	Published int   `db:"published"`
	CreatedAt int64 `db:"created_at"`
	UpdatedAt int64 `db:"updated_at"`
}

// Meta is what a source says about a photo that has no column of its own.
type Meta struct {
	Instagram *InstagramMeta `json:"instagram,omitempty"`
}

// InstagramMeta is what the instagram export knows about a post's media.
type InstagramMeta struct {
	URI           string `json:"uri,omitempty"`   // where it was in the export
	Index         int    `json:"index,omitempty"` // position in a carousel, from 1
	CrossPost     string `json:"cross_post,omitempty"`
	DeviceID      string `json:"device_id,omitempty"`
	Saves         int    `json:"saves,omitempty"`
	Shares        int    `json:"shares,omitempty"`
	Impressions   int    `json:"impressions,omitempty"`
	Reach         int    `json:"reach,omitempty"`
	ProfileVisits int    `json:"profile_visits,omitempty"`
	Follows       int    `json:"follows,omitempty"`
}

// Meta decodes the photo's source metadata.
func (p *Photo) Meta() Meta {
	var m Meta
	if p.MetaJSON != "" {
		json.Unmarshal([]byte(p.MetaJSON), &m)
	}
	return m
}

func (p *Photo) setMeta(m Meta) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	p.MetaJSON = string(b)
	return nil
}

// Taken is when the photo was taken, as the camera's wall clock (in UTC).
func (p *Photo) Taken() time.Time {
	if p.TakenAt <= 0 {
		return time.Time{}
	}
	return time.Unix(p.TakenAt, 0).UTC()
}

// Posted is when the photo was published at its source.
func (p *Photo) Posted() time.Time {
	if p.PostedAt <= 0 {
		return time.Time{}
	}
	return time.Unix(p.PostedAt, 0).UTC()
}

// HasGPS is true if the photo knows where it was taken.
func (p *Photo) HasGPS() bool { return p.Latitude != nil && p.Longitude != nil }

// Resolution is eg. "4032 × 3024", or "" if unknown.
func (p *Photo) Resolution() string {
	if p.Width == 0 || p.Height == 0 {
		return ""
	}
	return fmt.Sprintf("%d × %d", p.Width, p.Height)
}

// Camera is the make and model, without repeating the make when the model
// already starts with it ("Canon" + "Canon EOS R5" is "Canon EOS R5").
func (p *Photo) Camera() string {
	make, model := strings.TrimSpace(p.CameraMake), strings.TrimSpace(p.CameraModel)
	switch {
	case model == "":
		return make
	case make == "" || strings.HasPrefix(strings.ToLower(model), strings.ToLower(make)):
		return model
	}
	return make + " " + model
}

// ExposureSummary is eg. "50mm · f/1.8 · 1/60s · ISO 160", leaving out what
// isn't known.
func (p *Photo) ExposureSummary() string {
	var parts []string
	if p.FocalLength > 0 {
		parts = append(parts, trimFloat(p.FocalLength)+"mm")
	}
	if p.Aperture > 0 {
		parts = append(parts, "f/"+trimFloat(p.Aperture))
	}
	if p.Exposure != "" {
		parts = append(parts, p.Exposure+"s")
	}
	if p.ISO > 0 {
		parts = append(parts, fmt.Sprintf("ISO %d", p.ISO))
	}
	return strings.Join(parts, " · ")
}

func trimFloat(f float64) string {
	s := fmt.Sprintf("%.1f", f)
	return strings.TrimSuffix(s, ".0")
}

// An Album is a named group of photos, in an order.
type Album struct {
	ID          string `db:"id"`
	Slug        string `db:"slug"`
	Title       string `db:"title"`
	Description string `db:"description"`
	CoverID     string `db:"cover_id"`
	Published   int    `db:"published"`
	CreatedAt   int64  `db:"created_at"`
	UpdatedAt   int64  `db:"updated_at"`
}

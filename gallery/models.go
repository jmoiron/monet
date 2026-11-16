package gallery

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/jmoiron/monet/db"
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
		{
			// one row per import run: when, and what it did. Times are unix seconds.
			Up: `CREATE TABLE IF NOT EXISTS import_event (
				id integer PRIMARY KEY AUTOINCREMENT,
				source text NOT NULL,
				started_at integer NOT NULL,
				finished_at integer NOT NULL,
				posts integer NOT NULL DEFAULT 0,
				photos integer NOT NULL DEFAULT 0,
				created integer NOT NULL DEFAULT 0,
				updated integer NOT NULL DEFAULT 0,
				unchanged integer NOT NULL DEFAULT 0
			);`,
			Down: `DROP TABLE import_event;`,
		},
		{
			// What a model saw in a photo: a caption and labels (searched as words,
			// through photo_class_fts, which the import keeps in step) and/or an
			// embedding (searched by vector). One row per photo and model, so models
			// can be compared. The embedding is float16s, as in an export.
			Up: `CREATE TABLE IF NOT EXISTS photo_class (
				photo_id text NOT NULL,
				model text NOT NULL,
				caption text NOT NULL DEFAULT '',
				labels text NOT NULL DEFAULT '[]',
				embedding blob,
				created_at integer NOT NULL DEFAULT (strftime('%s', 'now')),
				PRIMARY KEY (photo_id, model)
			);
			CREATE VIRTUAL TABLE IF NOT EXISTS photo_class_fts USING fts5(
				photo_id UNINDEXED, model UNINDEXED, caption, labels, tokenize = 'porter unicode61'
			);`,
			Down: `DROP TABLE photo_class_fts; DROP TABLE photo_class;`,
		},
		{
			// Where a photo was taken, as names (from its coordinates, by pkg/geo):
			// the nearest city, its region, country and continent, and the other
			// cities near it, one to a line; and the city's name in the local
			// language. The words of it are searched, as the place of every row of
			// the photo in photo_class_fts, which gets the column for it here:
			// derived data, so it is made again from what is kept, and the places
			// are indexed when they are looked up.
			Up: `CREATE TABLE IF NOT EXISTS photo_place (
				photo_id text PRIMARY KEY,
				city text NOT NULL DEFAULT '',
				region text NOT NULL DEFAULT '',
				country text NOT NULL DEFAULT '',
				country_code text NOT NULL DEFAULT '',
				continent text NOT NULL DEFAULT '',
				near text NOT NULL DEFAULT '',
				local text NOT NULL DEFAULT '',
				local_lang text NOT NULL DEFAULT '',
				km real NOT NULL DEFAULT 0,
				scanned_at integer NOT NULL DEFAULT (strftime('%s', 'now'))
			);
			DROP TABLE photo_class_fts;
			CREATE VIRTUAL TABLE photo_class_fts USING fts5(
				photo_id UNINDEXED, model UNINDEXED, caption, labels, place, tokenize = 'porter unicode61'
			);
			INSERT INTO photo_class_fts (photo_id, model, caption, labels, place)
				SELECT photo_id, model, caption,
					coalesce((SELECT group_concat(json_extract(value, '$.name'), char(10)) FROM json_each(photo_class.labels)), ''), ''
				FROM photo_class;`,
			Down: `DROP TABLE photo_class_fts;
			CREATE VIRTUAL TABLE photo_class_fts USING fts5(
				photo_id UNINDEXED, model UNINDEXED, caption, labels, tokenize = 'porter unicode61'
			);
			INSERT INTO photo_class_fts (photo_id, model, caption, labels)
				SELECT photo_id, model, caption,
					coalesce((SELECT group_concat(json_extract(value, '$.name'), char(10)) FROM json_each(photo_class.labels)), '')
				FROM photo_class;
			DROP TABLE photo_place;`,
		},
		{
			// the region and the country in the language of the country, as the city has
			Up: `ALTER TABLE photo_place ADD COLUMN region_local text NOT NULL DEFAULT '';
				ALTER TABLE photo_place ADD COLUMN country_local text NOT NULL DEFAULT '';`,
			Down: `ALTER TABLE photo_place DROP COLUMN region_local;
				ALTER TABLE photo_place DROP COLUMN country_local;`,
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
	Filename  string         `json:"filename,omitempty"` // the name an upload had
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

// Filename is the photo's name where it came from: what an upload was called, or
// what instagram called the file. Failing both, the name it is stored under.
func (p *Photo) Filename() string {
	meta := p.Meta()
	switch {
	case meta.Filename != "":
		return meta.Filename
	case meta.Instagram != nil && meta.Instagram.URI != "":
		return path.Base(meta.Instagram.URI)
	}
	return path.Base(p.File)
}

// Resolution is eg. "4032x3024", or "" if unknown.
func (p *Photo) Resolution() string {
	if p.Width == 0 || p.Height == 0 {
		return ""
	}
	return fmt.Sprintf("%dx%d", p.Width, p.Height)
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

// recordImport keeps a note of an import that has finished: when it ran, and
// what it did.
func recordImport(d db.DB, source string, started time.Time, rep *ImportReport) error {
	_, err := d.Exec(`INSERT INTO import_event (source, started_at, finished_at, posts, photos, created, updated, unchanged)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		source, started.Unix(), time.Now().Unix(), rep.Posts, rep.Media, rep.Created, rep.Updated, rep.Unchanged)
	return err
}

// An ImportEvent is the record of one import that ran.
type ImportEvent struct {
	ID         int    `db:"id"`
	Source     string `db:"source"`
	StartedAt  int64  `db:"started_at"`
	FinishedAt int64  `db:"finished_at"`
	Posts      int    `db:"posts"`
	Photos     int    `db:"photos"`
	Created    int    `db:"created"`
	Updated    int    `db:"updated"`
	Unchanged  int    `db:"unchanged"`
}

// When is when the import started, for showing.
func (e ImportEvent) When() string { return time.Unix(e.StartedAt, 0).Format("Jan 2, 2006 · 15:04") }

// importEvents are the imports from source that have run, latest first.
func importEvents(d db.DB, source string) ([]ImportEvent, error) {
	var events []ImportEvent
	err := d.Select(&events, `SELECT * FROM import_event WHERE source=? ORDER BY started_at DESC, id DESC`, source)
	return events, err
}

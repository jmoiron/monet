package gallery

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

// sortTime is when a photo goes in time: when it was taken, else when it was
// posted, else when it was added. It is what photos are ordered by and what the
// date filters mean.
const sortTime = `CASE
	WHEN taken_at > 0 THEN taken_at
	WHEN posted_at > 0 THEN posted_at
	ELSE created_at END`

// What the GPS filter can ask for.
const (
	GPSHas   = "has"
	GPSLacks = "lacks"
)

const dateLayout = "2006-01-02"

var knownSources = map[string]bool{SourceUpload: true, SourceInstagram: true, SourceGPhotos: true}

// A Filter narrows the photos the admin shows. The zero Filter matches every
// photo.
type Filter struct {
	Source     string
	From, To   time.Time // whole days, UTC; To is the last day included. Zero for no limit
	Year       int       // a shortcut for the first to the last day of a year
	GPS        string    // GPSHas or GPSLacks: only photos that know where they were taken, or don't
	Collection string    // the id of an album the photo must be in
}

// ParseFilter reads a filter from a query string. What isn't understood is
// ignored, so a bad link shows everything rather than an error.
func ParseFilter(q url.Values) Filter {
	var f Filter
	if s := q.Get("source"); knownSources[s] {
		f.Source = s
	}
	if y, err := strconv.Atoi(q.Get("year")); err == nil && y >= 1800 && y <= 9999 {
		f.Year = y
		f.From = time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
		f.To = time.Date(y, 12, 31, 0, 0, 0, 0, time.UTC)
	}
	// explicit days win over a year
	if t, err := time.Parse(dateLayout, q.Get("from")); err == nil {
		f.From, f.Year = t, 0
	}
	if t, err := time.Parse(dateLayout, q.Get("to")); err == nil {
		f.To, f.Year = t, 0
	}
	if g := q.Get("gps"); g == GPSHas || g == GPSLacks {
		f.GPS = g
	}
	f.Collection = strings.TrimSpace(q.Get("collection"))
	return f
}

// IsZero is true if the filter matches everything.
func (f Filter) IsZero() bool { return f == Filter{} }

// Values is the filter as a query string, the inverse of ParseFilter.
func (f Filter) Values() url.Values {
	v := url.Values{}
	if f.Source != "" {
		v.Set("source", f.Source)
	}
	switch {
	case f.Year != 0:
		v.Set("year", strconv.Itoa(f.Year))
	default:
		if !f.From.IsZero() {
			v.Set("from", f.From.Format(dateLayout))
		}
		if !f.To.IsZero() {
			v.Set("to", f.To.Format(dateLayout))
		}
	}
	if f.GPS != "" {
		v.Set("gps", f.GPS)
	}
	if f.Collection != "" {
		v.Set("collection", f.Collection)
	}
	return v
}

// Query is Values as it is written on the end of a url: "" or "?source=upload".
func (f Filter) Query() string {
	if s := f.Values().Encode(); s != "" {
		return "?" + s
	}
	return ""
}

// FromDate and ToDate are the limits as the form fields show them.
func (f Filter) FromDate() string { return fmtTime(f.From, dateLayout) }
func (f Filter) ToDate() string   { return fmtTime(f.To, dateLayout) }

// Where is the filter as a where clause (with its arguments), which is empty
// for the zero Filter.
func (f Filter) Where() (string, []any) {
	var conds []string
	var args []any
	if f.Source != "" {
		conds = append(conds, `source = ?`)
		args = append(args, f.Source)
	}
	if !f.From.IsZero() {
		conds = append(conds, `(`+sortTime+`) >= ?`)
		args = append(args, f.From.Unix())
	}
	if !f.To.IsZero() {
		conds = append(conds, `(`+sortTime+`) < ?`)
		args = append(args, f.To.AddDate(0, 0, 1).Unix())
	}
	switch f.GPS {
	case GPSHas:
		conds = append(conds, `latitude IS NOT NULL AND longitude IS NOT NULL`)
	case GPSLacks:
		conds = append(conds, `(latitude IS NULL OR longitude IS NULL)`)
	}
	if f.Collection != "" {
		conds = append(conds, `id IN (SELECT photo_id FROM album_photo WHERE album_id = ?)`)
		args = append(args, f.Collection)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return `WHERE ` + strings.Join(conds, ` AND `) + ` `, args
}

// A YearCount is a year and how many photos are from it.
type YearCount struct {
	Year  int `db:"year"`
	Count int `db:"n"`
}

// Years lists the years there are photos from, newest first.
func (s *PhotoService) Years() ([]YearCount, error) {
	var years []YearCount
	err := s.db.Select(&years, `SELECT CAST(strftime('%Y', `+sortTime+`, 'unixepoch') AS integer) AS year, count(*) AS n
		FROM photo GROUP BY year ORDER BY year DESC`)
	return years, err
}

// A Spot is a photo on the map.
type Spot struct {
	ID    string  `json:"id"`
	Lat   float64 `json:"lat"`
	Lng   float64 `json:"lng"`
	Thumb string  `json:"thumb"`
	Title string  `json:"title"`
	Date  string  `json:"date"`
}

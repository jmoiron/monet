package gallery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/pkg/geo"
	"github.com/jmoiron/sqlx"
)

// the populations of the cities that a photo is placed by: the nearest of any
// size, the nearest of a few thousand people, and the nearest large city
var placePopulations = []int{0, 5000, 15000}

// A Place is where a photo was taken, by name.
type Place struct {
	PhotoID      string  `db:"photo_id"`
	City         string  `db:"city"`
	Region       string  `db:"region"`
	Country      string  `db:"country"`
	CountryCode  string  `db:"country_code"`
	Continent    string  `db:"continent"`
	Near         string  `db:"near"` // other cities near it, a line each
	Local        string  `db:"local"`
	LocalLang    string  `db:"local_lang"`
	RegionLocal  string  `db:"region_local"`
	CountryLocal string  `db:"country_local"`
	Km           float64 `db:"km"`
	ScannedAt    int64   `db:"scanned_at"`
}

// Words are what is searched of a place: every name there is of it.
func (p Place) Words() string {
	var w []string
	for _, s := range []string{p.City, p.Near, p.Local, p.Region, p.RegionLocal, p.Country, p.CountryLocal, p.Continent} {
		if s != "" {
			w = append(w, s)
		}
	}
	return strings.Join(w, "\n")
}

// A PlaceStatus says where the places of a gallery stand.
type PlaceStatus struct {
	Ready   bool   // the data is loaded, and photos can be placed
	Busy    string // what it is doing, if it is
	Err     string // why it isn't ready or what failed last
	Cities  int
	WithGPS int // photos that have coordinates
	Placed  int // of those, how many have a place
}

// Places names where photos were taken. The names come from pkg/geo's data of
// cities, which is fetched when it isn't there, and which can be fetched again
// when GeoNames has better.
type Places struct {
	db  db.DB
	dir string

	mu   sync.Mutex
	ix   *geo.Index
	busy string
	err  error
}

// NewPlaces makes the places of a gallery, with the data kept in dir. Without
// a dir photos aren't placed.
func NewPlaces(d db.DB, dir string) *Places { return &Places{db: d, dir: dir} }

// Enabled says if there is a dir for the data.
func (p *Places) Enabled() bool { return p != nil && p.dir != "" }

// Ensure has the data loaded and every photo placed: when the server starts,
// and so the data is fetched if it isn't there. It takes as long as the fetch.
func (p *Places) Ensure(ctx context.Context) {
	if !p.Enabled() {
		return
	}
	if !geo.Present(p.dir) {
		slog.Warn("no geonames data for places, fetching", "dir", p.dir)
		if err := p.Refresh(ctx); err != nil {
			slog.Error("fetching geonames data", "err", err)
		}
		return
	}
	if err := p.load(); err != nil {
		slog.Error("loading geonames data", "err", err)
		return
	}
	if n, err := p.Scan(false); err != nil {
		slog.Error("placing photos", "err", err)
	} else if n > 0 {
		slog.Info("placed photos", "count", n)
	}
}

func (p *Places) load() error {
	start := time.Now()
	ix, err := geo.Open(p.dir)
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		p.err = err
		return err
	}
	p.ix, p.err = ix, nil
	slog.Info("geonames data loaded", "cities", ix.Len(), "took", time.Since(start).Round(time.Millisecond))
	return nil
}

// begin says that a job is running; there is one at a time.
func (p *Places) begin(what string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.busy != "" {
		return fmt.Errorf("already %s", p.busy)
	}
	p.busy = what
	return nil
}

func (p *Places) end(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.busy = ""
	if err != nil {
		p.err = err
	}
}

// Refresh fetches the data again, and places every photo again with it.
func (p *Places) Refresh(ctx context.Context) (err error) {
	if !p.Enabled() {
		return errors.New("no directory for the geonames data")
	}
	if err := p.begin("fetching the data"); err != nil {
		return err
	}
	defer func() { p.end(err) }()
	if err := geo.Fetch(ctx, p.dir); err != nil {
		return err
	}
	if err := p.load(); err != nil {
		return err
	}
	_, err = p.scan(true)
	return err
}

// Scan places the photos that have coordinates: all of them, or those that have
// no place yet. It says how many it placed.
func (p *Places) Scan(all bool) (n int, err error) {
	if err := p.begin("placing photos"); err != nil {
		return 0, err
	}
	defer func() { p.end(err) }()
	return p.scan(all)
}

func (p *Places) scan(all bool) (int, error) {
	ix := p.index()
	if ix == nil {
		return 0, errors.New("the geonames data isn't loaded")
	}
	q := `SELECT id, latitude, longitude FROM photo WHERE latitude IS NOT NULL AND longitude IS NOT NULL`
	if !all {
		q += ` AND id NOT IN (SELECT photo_id FROM photo_place)`
	}
	var photos []struct {
		ID  string  `db:"id"`
		Lat float64 `db:"latitude"`
		Lng float64 `db:"longitude"`
	}
	if err := p.db.Select(&photos, q); err != nil {
		return 0, err
	}
	for _, ph := range photos {
		if err := p.store(ix, ph.ID, ph.Lat, ph.Lng); err != nil {
			return 0, err
		}
	}
	return len(photos), nil
}

func (p *Places) index() *geo.Index {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ix
}

// Place finds and keeps the place of a photo, if it has coordinates and the
// data is there to do it with. A photo that can't be placed yet is placed by
// the next scan.
func (p *Places) Place(ph *Photo) error {
	if !p.Enabled() || !ph.HasGPS() {
		return nil
	}
	ix := p.index()
	if ix == nil {
		return nil
	}
	return p.store(ix, ph.ID, *ph.Latitude, *ph.Longitude)
}

func (p *Places) store(ix *geo.Index, id string, lat, lng float64) error {
	around := ix.Around(lat, lng, placePopulations...)
	if len(around) == 0 {
		return nil
	}
	first := around[0]
	pl := Place{
		PhotoID: id, City: first.City, Region: first.Region, Country: first.Country,
		CountryCode: first.CountryCode, Continent: first.Continent,
		Local: first.Local, LocalLang: first.LocalLang, RegionLocal: first.RegionLocal, CountryLocal: first.CountryLocal,
		Km: first.Km,
	}
	var near []string
	for _, c := range around[1:] {
		near = append(near, c.City)
		if c.Local != "" {
			near = append(near, c.Local)
		}
	}
	pl.Near = strings.Join(near, "\n")
	return db.With(p.db, func(tx *sqlx.Tx) error {
		if _, err := tx.NamedExec(`INSERT OR REPLACE INTO photo_place
			(photo_id, city, region, country, country_code, continent, near, local, local_lang, region_local, country_local, km, scanned_at)
			VALUES (:photo_id, :city, :region, :country, :country_code, :continent, :near, :local, :local_lang, :region_local, :country_local, :km, strftime('%s', 'now'))`, pl); err != nil {
			return err
		}
		return reindex(tx, id)
	})
}

// Of is the place of a photo; nil if it has none.
func (p *Places) Of(photoID string) (*Place, error) {
	var pl Place
	err := p.db.Get(&pl, `SELECT * FROM photo_place WHERE photo_id=?`, photoID)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return nil, nil
		}
		return nil, err
	}
	return &pl, nil
}

// Status is where the places stand.
func (p *Places) Status() PlaceStatus {
	st := PlaceStatus{}
	if p == nil {
		return st
	}
	if p.Enabled() {
		p.mu.Lock()
		st.Busy = p.busy
		if p.err != nil {
			st.Err = p.err.Error()
		}
		if p.ix != nil {
			st.Ready, st.Cities = true, p.ix.Len()
		}
		p.mu.Unlock()
	}
	p.db.Get(&st.WithGPS, `SELECT count(*) FROM photo WHERE latitude IS NOT NULL AND longitude IS NOT NULL`)
	p.db.Get(&st.Placed, `SELECT count(*) FROM photo_place`)
	return st
}

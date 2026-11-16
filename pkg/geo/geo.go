// Package geo says where on earth a photo was taken, in the words people
// search with: the city, the region, the country and the continent. It uses
// the GeoNames lists of cities (https://download.geonames.org/export/dump/),
// which are small enough to hold in memory, so no coordinates leave the
// machine. The place is the nearest city in the list.
package geo

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// A Place is where a point is: the nearest city and what it is in.
type Place struct {
	City        string
	Region      string // a state, prefecture, province...
	Country     string
	CountryCode string
	Continent   string
	Population  int
	// Local is the city's name in the language of the country, when that isn't
	// the name above: 東京 for Tokyo. LocalLang is the language it is in. The
	// region and the country have theirs.
	Local, LocalLang          string
	RegionLocal, CountryLocal string
	// Km is how far the point is from the city; a long way means that the
	// list had nothing nearer, as at sea or in a desert.
	Km float64
}

// String is the place as "City, Region, Country".
func (p Place) String() string {
	var parts []string
	for _, s := range []string{p.City, p.Region, p.Country} {
		if s != "" && (len(parts) == 0 || parts[len(parts)-1] != s) {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

type city struct {
	id          int
	name, ascii string
	lat, lng    float64
	cos         float64 // of the latitude, for distances
	country     string  // code
	admin1      string  // code, within the country
	population  int
}

// An Index finds the nearest of a list of cities.
type Index struct {
	cities    []city
	countries map[string]country
	admin1    map[string]region // "JP.40" -> Tokyo
	local     map[int]localName // by geonameid, once LoadLocalNames has run
}

type region struct {
	name string
	id   int // the geonameid
}

type country struct {
	id              int // the geonameid
	name, continent string
	languages       []string // as GeoNames has them: "ja", "zh-CN", "en-IN"
}

var continents = map[string]string{
	"AF": "Africa", "AS": "Asia", "EU": "Europe", "NA": "North America",
	"OC": "Oceania", "SA": "South America", "AN": "Antarctica",
}

// Load reads a GeoNames cities file (a .zip of citiesNNNN.txt, or the .txt),
// with countryInfo.txt and admin1CodesASCII.txt, which are what the country
// and region codes in it mean.
func Load(cities, countryInfo, admin1 string) (*Index, error) {
	ix := &Index{countries: map[string]country{}, admin1: map[string]region{}}
	if err := lines(countryInfo, func(f []string) {
		if len(f) > 8 {
			c := country{name: f[4], continent: continents[f[8]]}
			if len(f) > 15 {
				c.languages = strings.Split(f[15], ",")
			}
			if len(f) > 16 {
				c.id, _ = strconv.Atoi(f[16])
			}
			ix.countries[f[0]] = c
		}
	}); err != nil {
		return nil, err
	}
	if err := lines(admin1, func(f []string) {
		if len(f) > 1 {
			r := region{name: f[1]}
			if len(f) > 3 {
				r.id, _ = strconv.Atoi(f[3])
			}
			ix.admin1[f[0]] = r
		}
	}); err != nil {
		return nil, err
	}
	err := lines(cities, func(f []string) {
		if len(f) < 15 {
			return
		}
		lat, err1 := strconv.ParseFloat(f[4], 64)
		lng, err2 := strconv.ParseFloat(f[5], 64)
		if err1 != nil || err2 != nil {
			return
		}
		pop, _ := strconv.Atoi(f[14])
		id, _ := strconv.Atoi(f[0])
		ix.cities = append(ix.cities, city{
			id: id, name: f[1], ascii: f[2], lat: lat, lng: lng, cos: math.Cos(lat * math.Pi / 180),
			country: f[8], admin1: f[10], population: pop,
		})
	})
	if err != nil {
		return nil, err
	}
	if len(ix.cities) == 0 {
		return nil, fmt.Errorf("%s: no cities", cities)
	}
	return ix, nil
}

// lines calls fn with the tab separated fields of each line of a file that
// isn't a comment. A .zip is read through the file named like it.
func lines(path string, fn func(fields []string)) error {
	var r io.Reader
	if strings.HasSuffix(path, ".zip") {
		zr, err := zip.OpenReader(path)
		if err != nil {
			return err
		}
		defer zr.Close()
		if len(zr.File) == 0 {
			return fmt.Errorf("%s: empty", path)
		}
		// the file named like the zip, as GeoNames has more than one in some
		file := zr.File[0]
		for _, zf := range zr.File {
			if zf.Name == strings.TrimSuffix(filepath.Base(path), ".zip")+".txt" {
				file = zf
			}
		}
		f, err := file.Open()
		if err != nil {
			return err
		}
		defer f.Close()
		r = f
	} else {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		r = f
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20) // alternate names make long lines
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		fn(strings.Split(line, "\t"))
	}
	return sc.Err()
}

// Len is the number of cities.
func (ix *Index) Len() int { return len(ix.cities) }

// Nearest is the place of the city nearest to a point.
func (ix *Index) Nearest(lat, lng float64) Place { return ix.nearest(lat, lng, 0) }

// Around is the places of the cities nearest to a point, among those of at
// least each of the populations: with the populations 0, 5000 and 15000 the
// place of the nearest town, of the nearest of a size, and of the nearest city.
// A city that is the nearest of several is only once in it, and the first
// is the nearest of all.
func (ix *Index) Around(lat, lng float64, minPops ...int) []Place {
	var out []Place
	seen := map[string]bool{}
	for _, pop := range minPops {
		p := ix.nearest(lat, lng, pop)
		if p.City == "" || seen[p.City] {
			continue
		}
		seen[p.City] = true
		out = append(out, p)
	}
	return out
}

func (ix *Index) nearest(lat, lng float64, minPop int) Place {
	best, bestD := -1, math.MaxFloat64
	for i := range ix.cities {
		c := &ix.cities[i]
		if c.population < minPop {
			continue
		}
		// the sort order of the distance is enough to find the nearest
		dlat := (c.lat - lat) * math.Pi / 180
		dlng := math.Remainder(c.lng-lng, 360) * math.Pi / 180
		d := dlat*dlat + dlng*dlng*c.cos*c.cos
		if d < bestD {
			best, bestD = i, d
		}
	}
	if best < 0 {
		return Place{}
	}
	c := ix.cities[best]
	co := ix.countries[c.country]
	reg := ix.admin1[c.country+"."+c.admin1]
	p := Place{
		City: c.name, Region: reg.name,
		Country: co.name, CountryCode: c.country, Continent: co.continent,
		Population: c.population, Km: haversine(lat, lng, c.lat, c.lng),
	}
	if l, ok := ix.local[c.id]; ok {
		p.Local, p.LocalLang = l.name, l.lang
	}
	p.RegionLocal = ix.local[reg.id].name
	p.CountryLocal = ix.local[co.id].name
	return p
}

// haversine is the distance in km between two points on the earth.
func haversine(lat1, lng1, lat2, lng2 float64) float64 {
	const r = 6371.0
	p1, p2 := lat1*math.Pi/180, lat2*math.Pi/180
	dp, dl := p2-p1, math.Remainder(lng2-lng1, 360)*math.Pi/180
	a := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}

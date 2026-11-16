// Command geo shows where the photos of a gallery were taken, by each of the
// GeoNames lists of cities, to choose the list to use: the smaller lists are
// quicker and have fewer odd places, the larger have the small towns.
//
//	make -C pkg/geo data
//	go run ./pkg/geo/cmd/geo -db prod.db
//
// The photos are the ones in the gallery's database that have coordinates.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	_ "github.com/mattn/go-sqlite3"

	"github.com/jmoiron/monet/pkg/geo"
)

func main() {
	dbPath := flag.String("db", "prod.db", "the gallery's sqlite database")
	data := flag.String("data", "pkg/geo/data", "where the GeoNames files are")
	alt := flag.String("alt", "alternateNamesV2.zip", "GeoNames' alternate names, in -data: read once, for the names in the local language")
	sizes := flag.String("lists", "1000,5000,15000", "the lists of cities to compare")
	flag.Parse()

	db, err := sql.Open("sqlite3", "file:"+*dbPath+"?mode=ro")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id, latitude, longitude FROM photo
		WHERE latitude IS NOT NULL AND longitude IS NOT NULL ORDER BY id`)
	if err != nil {
		log.Fatal(err)
	}
	type photo struct {
		id       string
		lat, lng float64
	}
	var photos []photo
	for rows.Next() {
		var p photo
		if err := rows.Scan(&p.id, &p.lat, &p.lng); err != nil {
			log.Fatal(err)
		}
		photos = append(photos, p)
	}
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}
	rows.Close()

	// the names in local languages are picked from the alternate names once,
	// for the cities of the largest list, and kept in a file
	local := filepath.Join(*data, "local-names.tsv")
	if _, err := os.Stat(local); err != nil {
		if _, err := os.Stat(filepath.Join(*data, *alt)); err == nil {
			big, err := geo.Load(filepath.Join(*data, "cities1000.zip"), filepath.Join(*data, "countryInfo.txt"), filepath.Join(*data, "admin1CodesASCII.txt"))
			if err != nil {
				log.Fatal(err)
			}
			fmt.Fprintf(os.Stderr, "picking local names from %s...\n", *alt)
			if err := big.LocalNames(filepath.Join(*data, *alt), local); err != nil {
				log.Fatal(err)
			}
		}
	}
	haveLocal := false
	if _, err := os.Stat(local); err == nil {
		haveLocal = true
	}

	lists := strings.Split(*sizes, ",")
	places := make([][]geo.Place, len(lists))
	for i, n := range lists {
		ix, err := geo.Load(filepath.Join(*data, "cities"+n+".zip"), filepath.Join(*data, "countryInfo.txt"), filepath.Join(*data, "admin1CodesASCII.txt"))
		if err != nil {
			log.Fatalf("%v (make -C pkg/geo data)", err)
		}
		if haveLocal {
			if err := ix.LoadLocalNames(local); err != nil {
				log.Fatal(err)
			}
		}
		fmt.Fprintf(os.Stderr, "%s: %d cities\n", n, ix.Len())
		for _, p := range photos {
			places[i] = append(places[i], ix.Nearest(p.lat, p.lng))
		}
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintf(tw, "image\t%s\n", strings.Join(lists, "\t"))
	var sameCity int
	for i, p := range photos {
		cells := make([]string, len(lists))
		for j := range lists {
			pl := places[j][i]
			cells[j] = fmt.Sprintf("%s, %s (%.0f km)", pl.String(), pl.Continent, pl.Km)
			if loc := localTuple(pl); loc != "" {
				cells[j] += " · " + loc
			}
		}
		fmt.Fprintf(tw, "%s\t%s\n", p.id, strings.Join(cells, "\t"))
		same := true
		for j := range lists {
			if places[j][i].City != places[0][i].City {
				same = false
			}
		}
		if same {
			sameCity++
		}
	}
	tw.Flush()
	fmt.Fprintf(os.Stderr, "%d photos with coordinates; the lists agree on the city for %d\n", len(photos), sameCity)
}

// localTuple is the names of a place in the language of its country, as the
// tuple of its English ones: "渋谷, 東京都, 日本", with what is missing left out.
func localTuple(p geo.Place) string {
	var parts []string
	for _, s := range []string{p.Local, p.RegionLocal, p.CountryLocal} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	if p.LocalLang == "" {
		return strings.Join(parts, ", ")
	}
	return strings.Join(parts, ", ") + " [" + p.LocalLang + "]"
}

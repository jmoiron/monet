package geo

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

type localName struct{ lang, name string }

// English is the language of the page, so it is not what a place is called
// locally; except where it is listed first for a country that has a language
// of its own that people write their cities in.
var localDespiteEnglish = map[string]bool{"IN": true, "SG": true, "PK": true}

// localLanguages are the languages a country's cities are named in, best
// first: what GeoNames lists for the country, without English, and none at
// all for a country that is first of all English speaking.
func localLanguages(cc string, c country) []string {
	var out []string
	for i, l := range c.languages {
		l = strings.ToLower(l)
		if primary(l) == "en" {
			if i == 0 && !localDespiteEnglish[cc] {
				return nil
			}
			continue
		}
		out = append(out, l)
	}
	return out
}

// primary is the language of a tag, without its country: "zh" of "zh-cn".
func primary(tag string) string {
	if i := strings.IndexAny(tag, "-_"); i > 0 {
		return tag[:i]
	}
	return tag
}

// LocalNames reads GeoNames' alternateNamesV2 (a .zip, or the .txt) and writes,
// for each city in the cities file and each region and country, its name in
// the first language of its country that has one, to out (geonameid, language
// and name, tab separated). That is a file small enough to load with every
// run, while reading the alternate names takes some time.
func (ix *Index) LocalNames(alternateNames, out string) error {
	type pick struct {
		rank      int
		preferred bool
		lang      string
		name      string
	}
	// what is named: by geonameid, the languages to take its name in, and the
	// names it has already, which are not local ones
	type entity struct {
		langs   []string
		english []string
	}
	ents := make(map[int]entity, len(ix.cities))
	add := func(id int, cc string, english ...string) {
		if id == 0 {
			return
		}
		if l := localLanguages(cc, ix.countries[cc]); len(l) > 0 {
			ents[id] = entity{langs: l, english: english}
		}
	}
	for i := range ix.cities {
		c := &ix.cities[i]
		add(c.id, c.country, c.name, c.ascii)
	}
	for code, r := range ix.admin1 {
		add(r.id, code[:strings.IndexByte(code, '.')], r.name)
	}
	for cc, c := range ix.countries {
		add(c.id, cc, c.name)
	}
	picks := map[int]pick{}
	err := lines(alternateNames, func(f []string) {
		if len(f) < 8 || f[6] == "1" || f[7] == "1" { // colloquial, historic
			return
		}
		id, err := strconv.Atoi(f[1])
		if err != nil {
			return
		}
		e, ok := ents[id]
		if !ok {
			return
		}
		lang := strings.ToLower(f[2])
		rank := -1
		for i, l := range e.langs {
			if lang == l || lang == primary(l) {
				rank = i
				break
			}
		}
		if rank < 0 || f[3] == "" {
			return
		}
		for _, en := range e.english {
			if f[3] == en {
				return
			}
		}
		cand := pick{rank: rank, preferred: f[4] == "1", lang: lang, name: f[3]}
		if cur, ok := picks[id]; !ok || cand.rank < cur.rank || cand.rank == cur.rank && cand.preferred && !cur.preferred {
			picks[id] = cand
		}
	})
	if err != nil {
		return err
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	ids := make([]int, 0, len(picks))
	for id := range picks {
		ids = append(ids, id)
	}
	sort.Ints(ids) // the same file for the same data
	for _, id := range ids {
		fmt.Fprintf(w, "%d\t%s\t%s\n", id, picks[id].lang, picks[id].name)
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// LoadLocalNames reads what LocalNames wrote, to have Nearest give local names.
func (ix *Index) LoadLocalNames(path string) error {
	ix.local = map[int]localName{}
	return lines(path, func(f []string) {
		if id, err := strconv.Atoi(f[0]); err == nil && len(f) >= 3 {
			ix.local[id] = localName{lang: f[1], name: f[2]}
		}
	})
}

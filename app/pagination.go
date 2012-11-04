package app

/* pagination helpers; ported from python code in github.com/jmoiron/jmoiron.net */

import (
	"github.com/jmoiron/monet/template"
	"math"
	"strconv"
)

type Link struct {
	Url    string
	Num    string
	HasUrl bool
	Inter  bool
}

type Paginator struct {
	Page       int
	PerPage    int
	Skip       int
	Limit      int
	Begin      int
	End        int
	Link       string
	WindowSize int
	HasPrev    bool
	HasNext    bool
	NumPages   int
	Inter      string
}

var defaultLink = ""
var defaultWindowSize = 10

func NewPaginator(page, perPage int) *Paginator {
	if page < 1 {
		return nil
	}
	p := new(Paginator)
	p.Page = page
	p.PerPage = perPage
	p.Skip = (page - 1) * perPage
	p.Begin = p.Skip
	p.End = p.Skip + perPage
	p.Limit = perPage
	p.Link = defaultLink
	p.WindowSize = defaultWindowSize
	p.Inter = "..."
	return p
}

/* given a maximum number of items, render pagination */
func (p *Paginator) Render(objCount int) string {
	if p.Begin > objCount {
		return ""
	}
	if p.Page > 1 {
		p.HasPrev = true
	}
	if p.End < objCount {
		p.HasNext = true
	}
	p.NumPages = int(math.Ceil(float64(objCount) / float64(p.PerPage)))
	if p.Page > p.NumPages {
		return ""
	}
	context := p.Context(p.NumPages)
	links := []Link{}
	for _, c := range context {
		v, _ := strconv.Atoi(c)
		if c == "..." {
			links = append(links, Link{Num: p.Inter, Inter: true})
		} else if v == p.Page {
			links = append(links, Link{Num: c})
		} else {
			links = append(links, Link{Num: c, Url: p.Link + c, HasUrl: true})
		}
	}
	return template.Render("paginator.mandira", M{"Pages": links}, p,
		M{
			"NextPageUrl": p.Link + itoa(p.Page+1),
			"PrevPageUrl": p.Link + itoa(p.Page-1),
		})
}

/* shortcut */
var itoa = strconv.Itoa

func strRange(begin, end int) []string {
	r := []string{}
	for i := begin; i < end; i++ {
		r = append(r, itoa(i))
	}
	return r
}

func makeWindow(center, size int) []string {
	lpad := size / 2
	rpad := size / 2
	if size%2 >= 0 {
		lpad = size/2 - 1
	}
	return strRange(center-lpad, center+rpad+1)
}

func (p *Paginator) Context(pageCount int) []string {
	padArea := int(2 + math.Ceil(float64(p.WindowSize-4)/2.0))
	middle := p.WindowSize - 4

	if pageCount < p.WindowSize {
		return strRange(1, pageCount+1)
	}

	if p.Page <= padArea {
		r := strRange(1, 2+middle+1)
		for _, s := range []string{p.Inter, itoa(pageCount - 1), itoa(pageCount)} {
			r = append(r, s)
		}
		return r
	}
	if pageCount-padArea <= p.Page && p.Page <= pageCount {
		r := strRange(1, 3)
		r = append(r, p.Inter)
		for _, s := range strRange(pageCount-middle-1, pageCount+1) {
			r = append(r, s)
		}
		return r
	}
	r := strRange(1, 3)
	r = append(r, p.Inter)
	for _, s := range makeWindow(p.Page, middle) {
		r = append(r, s)
	}
	r = append(r, p.Inter)
	r = append(r, itoa(pageCount-1))
	r = append(r, itoa(pageCount))
	return r
}

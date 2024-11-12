package mtr

// support for pagination

import (
	"bytes"
	"embed"
	"html/template"
	"log/slog"
	"math"
	"path"
	"strconv"
)

var defaultLink = ""
var defaultWindowSize = 10

type LinkFn func(int) string

//go:embed mtr/*
var paginationTemplate embed.FS

// SlashLinkFn returns a LinkFn that adds a number element to a base path,
// eg. SlashLinkFn("/blog/page") returns a LinkFn that generates urls like
// "/blog/page/3".
func SlashLinkFn(base string) LinkFn {
	return func(p int) string {
		return path.Join(base, strconv.Itoa(p))
	}
}

type Page struct {
	Number      int
	StartOffset int
	EndOffset   int
	HasPrevious bool
	HasNext     bool
}

// A Paginator assists in pagination by providing HTML rendering for pagination
// as well as windowing for fetching the correct items.
type Paginator struct {
	// PageSize is the number of elements per page
	PageSize int
	// Total is the total number of elements
	Total int
	// NumPages is the number of total pages
	NumPages int
	// WindowSize is the maximum number of pages to render at once
	// If there are more than WindowSize pages, then some pages will
	// be omitted during rendering and elipsized, eg:
	// <- 1 2 3 ... 14 15 16 ->
	WindowSize int
	// Ellipsis is inserted between the start and end if the number of
	// pages is more than the Windowsize
	Ellipsis string

	// href is used to generate urls from pages
	href LinkFn
}

func NewPaginator(pageSize, objCount int) *Paginator {
	return &Paginator{
		PageSize:   pageSize,
		Total:      objCount,
		NumPages:   int(math.Ceil(float64(objCount) / float64(pageSize))),
		WindowSize: defaultWindowSize,
		Ellipsis:   "...",
		href:       SlashLinkFn("/"),
	}
}

// WithLinkFn sets the paginator's url generating function to fn
func (p *Paginator) WithLinkFn(fn LinkFn) *Paginator {
	p.href = fn
	return p
}

// Page at ordinal num
func (p *Paginator) Page(num int) *Page {
	// XXX: do we return an error if the page is off the end?
	return &Page{
		Number:      num,
		StartOffset: (num - 1) * p.PageSize,
		EndOffset:   num * p.PageSize,
		HasPrevious: num > 1,
		HasNext:     num*p.PageSize < p.Total,
	}
}

func (p *Paginator) Render(reg *Registry, page *Page) template.HTML {
	if page.StartOffset > p.Total {
		return template.HTML("")
	}

	window := p.makeWindow(page)

	var b bytes.Buffer
	// XXX: error

	err := reg.Render(&b, "mtr/pagination.html", Ctx{
		"paginator": p,
		"page":      page,
		"pages":     window,
		"prevUrl":   p.href(page.Number - 1),
		"nextUrl":   p.href(page.Number + 1),
	})
	if err != nil {
		slog.Error("rendering paginator", "err", err)
	}

	return template.HTML(b.String())
}

// a pageLink represents an element in the rendered paginator
type pageLink struct {
	Url        string
	Text       string
	IsEllipsis bool
}

func newPageLink(url, text string, isEllipsis bool) pageLink {
	return pageLink{Url: url, Text: text, IsEllipsis: isEllipsis}
}

// makeWindow returns a set of pageLinks for this paginator for this page
// these encode all of the logic needed to easily render the paginator
func (p *Paginator) makeWindow(page *Page) []pageLink {
	var links []pageLink

	// we want to draw a constant-width paginator regardless of what page we
	// are on and how many total pages there are.  Therefore, we generate a
	// window of links of the desired size, and then decide what kind of
	// context around the current page we want to render.
	//
	// there are 4 scenarios:
	//
	// * we draw all of the pages because they're less than the window size
	// * the current page is close enough to the beginning we have one ...
	// * the current page is close enough to the end that we have one ...
	// * the current page is in the middle of enough pages that we have two ...
	//
	// the first scenario is the simplest so we do that first;  if we have a
	// bigger window than the number of pages, return them all w/o any ellipsis
	if p.NumPages < p.WindowSize {
		// pages are 1-indexed
		for pn := 1; pn < p.NumPages+1; pn++ {
			l := newPageLink(p.href(pn), strconv.Itoa(pn), false)
			// if this "link" is the current page, don't add a link
			if pn == page.Number {
				l.Url = ""
			}
			links = append(links, l)
		}
		return links
	}
	return links

	/*

		middle := p.WindowSize - 4
		padArea := int(2 + math.Ceil(float64(middle)/2.0))

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
	*/

}

/* given a maximum number of items, render pagination
func (p *Paginator) Render() string {
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
*/

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

/*
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

*/

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
		href:       SlashLinkFn("./"),
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

	// if this "link" is the current page, don't add a link
	stripCurrent := func(l pageLink, num int) pageLink {
		if num == page.Number {
			l.Url = ""
		}
		return l
	}

	edge := func(ls ...string) (links []pageLink) {
		for _, l := range ls {
			if l == p.Ellipsis {
				links = append(links, newPageLink("", p.Ellipsis, true))
				continue
			}
			pn, _ := strconv.Atoi(l)
			links = append(links, newPageLink(p.href(pn), l, false))
		}
		return
	}

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
			links = append(links, stripCurrent(l, pn))
		}
		return links
	}

	// determine if we're in the middle two scenarios; we're close enough
	// to an edge to only have one ellipsis
	middle := p.WindowSize - 4

	// we set aside 2 "pages" on the left and right hand side
	// the full padding area where a page can be considered to be "close enough"
	// to the left or right hand side is 2 + 1/2 the size of the middle, as we
	// do not want to add ellipsis between adjacent numbers
	padArea := int(2 + math.Ceil(float64(middle)/2.0))

	// page is close enough to the start
	if page.Number <= padArea {
		for pn := 1; pn < middle+3; pn++ {
			l := newPageLink(p.href(pn), strconv.Itoa(pn), false)
			links = append(links, stripCurrent(l, pn))
		}
		// now add the ellipsis and the last two pages
		links = append(links, edge(p.Ellipsis, strconv.Itoa(p.NumPages-1), strconv.Itoa(p.NumPages))...)
		return links
	}

	// page is close enough to the end
	if page.Number >= p.NumPages-padArea {
		links = append(links, edge("1", "2", p.Ellipsis)...)

		for pn := p.NumPages - middle - 1; pn <= p.NumPages; pn++ {
			l := newPageLink(p.href(pn), strconv.Itoa(pn), false)
			links = append(links, stripCurrent(l, pn))
		}
		return links
	}

	// otherwise, we render with the current page in the "middle" of the window
	links = append(links, edge("1", "2", p.Ellipsis)...)
	// add the middle
	start, end := makeWindow(page.Number, middle)
	for pn := start; pn < end; pn++ {
		l := newPageLink(p.href(pn), strconv.Itoa(pn), false)
		links = append(links, stripCurrent(l, pn))
	}

	links = append(links, edge(p.Ellipsis, strconv.Itoa(p.NumPages-1), strconv.Itoa(p.NumPages))...)
	return links
}

func makeWindow(center, size int) (start, end int) {
	lpad := size / 2
	rpad := size / 2
	if size%2 >= 0 {
		lpad = size/2 - 1
	}
	return center - lpad, center + rpad + 1
}

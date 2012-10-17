package app

import (
	"github.com/hoisie/web"
	"github.com/jmoiron/monet/template"
	"strconv"
	"time"
)

type M map[string]interface{}

var base = template.Base{Path: "base.mustache"}

func GetPost(route string, handler interface{}) {
	web.Get(route, handler)
	web.Post(route, handler)
}

/* Should have this to be an app. */
func Attach(url string) {

}

// Return a number for a page (default to 1)
func PageNumber(page string) int {
	num := 1
	if len(page) > 0 {
		num, _ = strconv.Atoi(page)
	}
	return num
}

// Format a timestamp into a simple date
func FmtTimestamp(ts uint64) string {
	now := time.Now()
	ut := time.Unix(int64(ts), 0)
	if now.Year() == ut.Year() {
		return ut.Format("Jan _2")
	}
	return ut.Format("Jan _2 2006")
}

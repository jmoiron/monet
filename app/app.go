// package app provides a composition based component framework for building
// a personal site out of distinct sub-applications.
//
// An example of an application is a blog or a photo gallery.
//
// The app package tries to balance being opinionated and being flexible.

package app

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/monet/mtr"
)

// things to be opinionated about:
// - binding to a router
// - template system/engine
// - db + migrations
// - users/auth
// - having an admin site to bind to; not enough introspection to make an "admin" app

// Bindable items can bind their URL routes to a router.
type Bindable interface {
	Bind(r chi.Router)
}

// An App is a component that controls a part of a website.
// An example of an App is a wiki, blog, gallery, etc.
type App interface {
	Bindable
	Name() string
	Migrate() error
	Register(*mtr.Registry)
	GetAdmin() (Admin, error)
}

// An Admin is a component that allows a user to administer a website.
//
// An example is editing flatpages, wiki pages, blog posts, uploading photos
// to a gallery, etc.
type Admin interface {
	Bindable
	// generate one or more panels for the admin index
	Panels(*http.Request) ([]string, error)
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
func FmtTimestamp(ts int64) string {
	now := time.Now()
	ut := time.Unix(ts, 0)
	if now.Year() == ut.Year() {
		return ut.Format("Jan _2")
	}
	return ut.Format("Jan _2 2006")
}

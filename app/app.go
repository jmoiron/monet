// package app provides a composition based component framework for building
// a personal site out of distinct sub-applications.
//
// An example of an application is a blog or a photo gallery.
//
// The app package tries to balance being opinionated and being flexible.

package app

import (
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
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

// A Renderer can render itself when called.
type Renderer interface {
	Render() (string, error)
}

// An App is a component that controls a part of a website.
// An example of an App is a wiki, blog, gallery, etc.
type App interface {
	Bindable
	Name() string
	Migrate() error
	GetAdmin() (Admin, error)
}

// An Admin is a component that allows a user to administer a website.
//
// An example is editing flatpages, wiki pages, blog posts, uploading photos
// to a gallery, etc.
type Admin interface {
	Bindable
	// WritePanel(io.Writer) error
	Panel() Renderer
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

// Attempt to PrettyPrint some things.  Returns a string.
func PrettyPrint(i interface{}) string {
	limit := 200
	v := reflect.ValueOf(i)
	switch v.Kind() {
	case reflect.String:
		return fmt.Sprintf("\"%s\"", v.String())
	case reflect.Ptr:
		e := v.Elem()
		if !e.IsValid() {
			return fmt.Sprintf("%#v", i)
		} else {
			return fmt.Sprintf("&%s", PrettyPrint(e.Interface()))
		}
	case reflect.Slice:
		s := fmt.Sprintf("%#v", i)
		if len(s) < limit {
			return s
		}
		return s[:limit] + "..."
	case reflect.Struct:
		t := v.Type()
		s := "{\n"
		for i := 0; i < v.NumField(); i++ {
			if f := t.Field(i); f.Name != "" {
				s += fmt.Sprintf("\t\"%s\": %s\n", f.Name, PrettyPrint(v.Field(i).Interface()))
			}
		}
		s += "}\n"
		return s
	default:
		return fmt.Sprintf("%#v", i)
	}
	return ""
}

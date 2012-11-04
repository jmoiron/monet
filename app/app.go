package app

import (
	"fmt"
	"github.com/hoisie/web"
	"github.com/jmoiron/monet/template"
	"reflect"
	"strconv"
	"time"
)

type M map[string]interface{}

var base = template.Base{Path: "base.mandira"}

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

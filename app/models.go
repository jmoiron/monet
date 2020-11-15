package app

import (
	"regexp"
	"strings"
)

// -- Utilities --

var (
	stripspace  = regexp.MustCompile("[^\\w\\s\\-]")
	dashreplace = regexp.MustCompile("[^\\w]+")
)

// Slugify some text.  Do not strip words as django does, but do collapse
// spaces and use dashes in favor of all other non-alphanum characters.
func Slugify(text string) string {
	s := text
	s = stripspace.ReplaceAllString(s, "")
	s = dashreplace.ReplaceAllString(s, "-")
	s = strings.Replace(s, "_", "-", -1)
	return strings.ToLower(s)
}

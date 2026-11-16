package classify

import (
	"strings"
	"unicode"
)

// cleanText is a query as open_clip's SigLIP tokenizer has it before it is
// tokenized ("canonicalize"): "_" as a space, the ASCII punctuation out, lower
// case, and one space between words. The models were trained on texts like
// that, so a query has to be too to land where the training put it. (It also
// fixes mojibake and html entities first; that isn't done here.)
func cleanText(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.Map(func(r rune) rune {
		if r < unicode.MaxASCII && (unicode.IsPunct(r) || unicode.IsSymbol(r)) {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

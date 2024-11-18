package db

import "strings"

// helper routines for fts5 searching

/*
By enclosing it in double quotes ("). Within a string, any embedded double quote
characters may be escaped SQL-style - by adding a second double-quote character.

As an FTS5 bareword that is not "AND", "OR" or "NOT" (case sensitive). An FTS5
bareword is a string of one or more consecutive characters that are all either:

 * Non-ASCII range characters (i.e. unicode codepoints greater than 127), or
 * One of the 52 upper and lower case ASCII characters, or
 * One of the 10 decimal digit ASCII characters, or
 * The underscore character (unicode codepoint 96).
 * The substitute character (unicode codepoint 26).

Strings that include any other characters must be quoted. Characters that are not
currently allowed in barewords, are not quote characters and do not currently serve
any special purpose in FTS5 query expressions may at some point in the future be
allowed in barewords or used to implement new query functionality. This means that
queries that are currently syntax errors because they include such a character outside
of a quoted string may be interpreted differently by some future version of FTS5.
*/

type filterFn func([]string) []string

// SafeQuery makes a query string safe to use with an fts5 match.
//
// There are queries that make sense (eg; the query `c++`) that are currently syntax
// errors because of the characters they contain.
//
// With the trigram fts5 tokenizer, a quoted token is searched in the same way as a
// bareword (ie. `foo` and `"foo"` return the same results), so our approach is to
// quote all unquoted barewords, save for the special tokens `AND`, `OR`, and `NOT`.
//
// Regarding logical operators, there are two usability issues with them:
//   - A hanging operator (eg. `foo AND NOT` or simply `NOT`) will cause an error
//   - Only all-uppercase versions of these operators work as operators
//
// The approach is to match operators in a case-insensitive way and convert matches
// to uppercase, and then if the final token is an operator, quote it.
func SafeQuery(query string) string {
	tokens := tokenize(query)

	filters := []filterFn{
		convertOperators,
		autoQuote,
		fixOperator,
	}

	for _, fn := range filters {
		tokens = fn(tokens)
	}

	return strings.Join(tokens, " ")
}

// Tokenize returns the query split up into tokens.  Tokens are space separated runs
// of characters.
//
// Tokens may be quoted in the original text; quoting is respected but it is not
// preserved in the output, as all tokens will get auto-quoted later.
//
// Two quotes together escape a quotation mark, but only if it's within a quoted
// token.
func tokenize(query string) []string {
	var (
		tokens   []string
		inQuote  bool
		inEscape bool
		inToken  bool
		tok      []rune
		rq       = []rune(query)
	)

	for i, c := range rq {
		switch c {
		case '"':
			switch {
			case !inToken:
				inQuote = true
				inToken = true
			case !inQuote:
				if len(tok) > 0 {
					tokens = append(tokens, string(tok))
					tok = tok[:0]
				}
				inQuote = true
			case inEscape:
				tok = append(tok, c)
				inEscape = false
			case len(rq) == i+1:
				continue
			case rq[i+1] == '"':
				inEscape = true
				tok = append(tok, c)
			case inQuote:
				inToken = false
				inQuote = false
				tokens = append(tokens, string(tok))
				tok = tok[:0]
			}
		case ' ':
			switch {
			case inQuote:
				tok = append(tok, c)
			case inToken:
				inToken = false
				tokens = append(tokens, string(tok))
				tok = tok[:0]
			}
		default:
			inToken = true
			tok = append(tok, c)
		}
	}
	if len(tok) > 0 {
		tokens = append(tokens, string(tok))
	}
	return tokens
}

// convertOperators case insensitively converts operators to their uppercase form
func convertOperators(tokens []string) []string {
	var out []string
	for _, tok := range tokens {
		upper := strings.ToUpper(tok)
		switch upper {
		case "NOT", "OR", "AND":
			out = append(out, upper)
		default:
			out = append(out, tok)
		}
	}
	return out
}

// autoQuote all of the tokens
func autoQuote(tokens []string) []string {
	var out []string
	for _, tok := range tokens {
		// do not quote operators
		switch tok {
		case "NOT", "OR", "AND":
			out = append(out, tok)
		default:
			out = append(out, `"`+tok+`"`)
		}
	}
	return out
}

// fixOperator fixes hanging operators in the token list
func fixOperator(tokens []string) []string {
	last := tokens[len(tokens)-1]
	switch last {
	case "NOT", "OR", "AND":
		var tok []string
		tok = append(tok, tokens...)
		tok[len(tokens)-1] = `"` + last + `"`
		return tok
	}
	return tokens
}

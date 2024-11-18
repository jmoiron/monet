package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTokenizer(t *testing.T) {
	assert := assert.New(t)

	type tc struct {
		in  string
		out []string
	}
	cases := []tc{
		{`foo`, []string{"foo"}},
		{`"foo"`, []string{`foo`}},
		{`foo"`, []string{`foo`}},
		{`"foo`, []string{`foo`}},
		{`"foo bar baz`, []string{"foo bar baz"}},
		{`"foo""`, []string{`foo""`}},
		{`foo""`, []string{`foo`}},
		{`"foo""bar"`, []string{`foo""bar`}},
		{`hello world`, []string{"hello", "world"}},
		{`what's that`, []string{"what's", "that"}},
		{`abc def ghi`, []string{"abc", "def", "ghi"}},
	}

	for _, c := range cases {
		res := tokenize(c.in)
		assert.Equal(c.out, res)
	}
}

func TestSafeQuery(t *testing.T) {
	assert := assert.New(t)

	type tc struct{ in, out string }
	cases := []tc{
		{`foo`, `"foo"`},
		{`"foo"`, `"foo"`},
		{"hello world", `"hello" "world"`},
		{"x and y", `"x" AND "y"`},
		{"c++", `"c++"`},
		{`x and y and not`, `"x" AND "y" AND "NOT"`},
	}

	for _, c := range cases {
		res := SafeQuery(c.in)
		assert.Equal(c.out, res, c.in)
	}
}

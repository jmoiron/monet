package template

import (
	"github.com/gomarkdown/markdown"
	"github.com/jmoiron/monet/conf"
)

type dict map[string]interface{}

type Renderer struct {
	base   string
	config conf.Config
}

func RenderMarkdown(source string) string {
	out := markdown.ToHTML([]byte(source), nil, nil)
	return string(out)

	/*
		extensions |= blackfriday.EXTENSION_NO_INTRA_EMPHASIS
		extensions |= blackfriday.EXTENSION_TABLES
		extensions |= blackfriday.EXTENSION_FENCED_CODE
		extensions |= blackfriday.EXTENSION_AUTOLINK
		extensions |= blackfriday.EXTENSION_STRIKETHROUGH
		extensions |= blackfriday.EXTENSION_SPACE_HEADERS
		extensions |= blackfriday.EXTENSION_HARD_LINE_BREAK
		flags |= blackfriday.HTML_SAFELINK
		renderer := blackfriday.HtmlRenderer(flags, "", "")
		return string(blackfriday.Markdown([]byte(source), renderer, extensions))
	*/
}

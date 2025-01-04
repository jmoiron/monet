package main

import (
	"bytes"
	"syscall/js"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

func main() {
	c := make(chan struct{})

	js.Global().Set("toHtml", js.FuncOf(jsToHtml))

	<-c
}

func jsToHtml(this js.Value, args []js.Value) any {
	out := toHtml(args[0].String())
	return out
	//js.ValueOf(out)
}

func toHtml(src string) string {
	var parser = goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithRendererOptions(
			html.WithUnsafe(),
		),
	)

	var buf bytes.Buffer
	parser.Convert([]byte(src), &buf)
	return buf.String()
}

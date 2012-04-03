package template

import (
    "fmt"
    "os"
    "strings"
    "path/filepath"
    "github.com/hoisie/mustache.go"
    "github.com/russross/blackfriday"
)

type dict map[string]string

type Base struct {
    Path string
}

var templates = map[string] *mustache.Template{}

func (b *Base) Render(t string, c ...interface{}) string {
    body := Render(t, c...)
    newc := append(c, dict{"body":body})
    return Render(b.Path, newc...)
}

func Render(t string, c ...interface{}) string {
    template := templates[t]
    if template != nil {
        return template.Render(c...)
    }
    fmt.Printf("Error: template %s not found\n", t)
    return ""
}

func LoadDir(dir string) {
    walker := func(path string, info os.FileInfo, err error) error {
        name, isDir := info.Name(), info.IsDir()
        if isDir { return nil }
        if name[0] == '.' { return nil }
        tname := strings.Join(strings.Split(path, "/")[1:], "/")
        templates[tname], err = mustache.ParseFile(path)
        if err != nil {
            fmt.Println(err)
        } else {
            fmt.Printf(" * %v\n", tname)
        }
        return nil
    }
    filepath.Walk(dir, walker)
}

func Init(templatePaths []string) {
    for _,path := range templatePaths {
        LoadDir(path)
    }
}

func RenderMarkdown(source string) string {
    var flags int
    var extensions int
    extensions |= blackfriday.EXTENSION_NO_INTRA_EMPHASIS
    extensions |= blackfriday.EXTENSION_TABLES
    extensions |= blackfriday.EXTENSION_FENCED_CODE
	extensions |= blackfriday.EXTENSION_AUTOLINK
	extensions |= blackfriday.EXTENSION_STRIKETHROUGH
	extensions |= blackfriday.EXTENSION_SPACE_HEADERS
	extensions |= blackfriday.EXTENSION_HARD_LINE_BREAK
    flags |= blackfriday.HTML_GITHUB_BLOCKCODE
    flags |= blackfriday.HTML_SAFELINK
    renderer := blackfriday.HtmlRenderer(flags, "", "")
    return string(blackfriday.Markdown([]byte(source), renderer, extensions))
}


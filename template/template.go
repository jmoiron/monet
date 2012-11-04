package template

import (
	"fmt"
	"github.com/jmoiron/mandira"
	"github.com/jmoiron/monet/conf"
	"github.com/russross/blackfriday"
	"os"
	"path/filepath"
	"strings"
)

type dict map[string]interface{}

type Base struct {
	Path string
}

var templates = map[string]*mandira.Template{}
var templatePaths = map[string]string{}

func (b *Base) Render(t string, c ...interface{}) string {
	body := Render(t, c...)
	newc := append(c, dict{"body": body})
	return Render(b.Path, newc...)
}

func Render(t string, c ...interface{}) string {
	// add the Config to all of our template rendering
	c = append(c, dict{"Debug": conf.Config.Debug})
	c = append(c, dict{"Config": conf.Config})

	if conf.Config.TemplatePreCompile {
		template := templates[t]
		if template == nil {
			fmt.Printf("Error: template %s not found\n", t)
			return ""
		}
		return template.Render(c...)
	}
	path := templatePaths[t]
	if len(path) < 0 {
		fmt.Printf("Error: template %s not found\n", t)
		return ""
	}
	template, err := mandira.ParseFile(path)
	if err != nil {
		fmt.Println(err)
		return ""
	}
	return template.Render(c...)
}

func LoadDir(dir string) int {
	numTemplates := 0
	walker := func(path string, info os.FileInfo, err error) error {
		name, isDir := info.Name(), info.IsDir()
		if isDir {
			return nil
		}
		if name[0] == '.' {
			return nil
		}
		tname := strings.Join(strings.Split(path, "/")[1:], "/")
		templatePaths[tname] = path
		// if template pre-compilation is on, compile these and store
		if conf.Config.TemplatePreCompile {
			templates[tname], err = mandira.ParseFile(path)
			if err != nil {
				fmt.Printf("%s: %v\n", tname, err)
			}
		}
		numTemplates++
		return nil
	}
	filepath.Walk(dir, walker)
	return numTemplates
}

func Init(templatePaths []string) {
	for _, path := range templatePaths {
		num := LoadDir(path)
		fmt.Printf("Loaded %d templates from %s\n", num, path)
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

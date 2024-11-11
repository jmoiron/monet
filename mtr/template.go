// Package mtr is the monet template registry.
// Please stand back from the doors.
package mtr

import (
	"bytes"
	"context"
	"html/template"
	"io"
	"io/fs"
	"sync"

	"github.com/gomarkdown/markdown"
)

type regKey struct{}

type Ctx map[string]any

type Registry struct {
	base *registry
	tmpl *registry
}

// NewRegistry returns a new, empty template registry.
// You probably only want to have one of these per instance.
func NewRegistry() *Registry {
	return &Registry{
		base: newRegistry(),
		tmpl: newRegistry(),
	}
}

func (r *Registry) AddBase(name string, tmpl *template.Template) {
	r.base.add(name, tmpl)
}

func (r *Registry) Add(name string, tmpl *template.Template) {
	r.tmpl.add(name, tmpl)
}

func (r *Registry) AddBaseFS(name string, fs fs.FS) error {
	tmpl, err := template.New(name).ParseFS(fs, name)
	if err != nil {
		return err
	}
	r.AddBase(name, tmpl)
	return nil
}

// AddFS adds the named template from the given filesystem.
func (r *Registry) AddFS(name string, fs fs.FS) error {
	tmpl, err := template.New(name).ParseFS(fs, name)
	if err != nil {
		return err
	}
	r.Add(name, tmpl)
	return nil
}

// Render the named template name to w using the provided context.
func (r *Registry) Render(w io.Writer, name string, ctx Ctx) error {
	content := r.tmpl.get(name)
	return content.Execute(w, ctx)
}

// RenderWithBase renders the template 'name' with the base template 'base' to the writer,
// using the provided context.  The base template template will receive the child template's
// content as 'body', which it should incorporate into its structure.
func (r *Registry) RenderWithBase(w io.Writer, base, name string, ctx Ctx) error {
	baseTpl := r.base.get(base)
	content := r.tmpl.get(name)

	var s bytes.Buffer
	err := content.Execute(&s, ctx)
	if err != nil {
		return err
	}

	ctx["body"] = s.String()
	if err != nil {
		return err
	}

	return baseTpl.Execute(w, ctx)
}

// Context adds this registry to the context.
func (r *Registry) Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, regKey{}, r)
}

// a registry is a concurrent-safe map of string->*template.Template.
type registry struct {
	reg map[string]*template.Template
	mu  sync.RWMutex
}

func (r *registry) add(name string, tmpl *template.Template) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reg[name] = tmpl
}

func (r *registry) del(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.reg, name)
}

func (r *registry) get(name string) *template.Template {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.reg[name]
}

func newRegistry() *registry {
	return &registry{reg: make(map[string]*template.Template, 10)}
}

// RegistryFromContext returns the registry from the ctx. It should have been added
// with Registry.Context(ctx).
func RegistryFromContext(ctx context.Context) *Registry {
	return ctx.Value(regKey{}).(*Registry)
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

// Package mtr is the monet template registry.
// Please stand back from the doors.
package mtr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	gostrings "strings"
	"sync"

	"github.com/go-sprout/sprout"
	"github.com/go-sprout/sprout/registry/maps"
	"github.com/go-sprout/sprout/registry/numeric"
	"github.com/go-sprout/sprout/registry/slices"
	"github.com/go-sprout/sprout/registry/std"
	"github.com/go-sprout/sprout/registry/strings"
	"github.com/gomarkdown/markdown"
)

type regKey struct{}

// A Ctx defines the data available to a template at rendering time.
type Ctx map[string]any

// A Registry manages a set of templates from various apps, providing a way
// for them to share features with eachother.
//
// Create an empty registry with NewRegistry, and then use the Add/AddBase*
// functions to add named templates to the registry. Once all of the templates
// have been added, call Registry.Build(), which builds the function map and
// parses all of the templates.
//
// You can render templates directly with Render, or render them within the
// context of a base template with RenderWithBase.
type Registry struct {
	base *registry
	tmpl *registry

	// synchronized set of deferred templates
	dts []deferredTemplate
	mu  sync.Mutex

	// Handler is a sprout handler.
	Handler sprout.Handler
}

type deferredTemplate struct {
	name, path string
	fs         fs.FS
	isBase     bool
}

// NewRegistry returns a new, empty template registry.
func NewRegistry() *Registry {
	reg := &Registry{
		base: newRegistry(),
		tmpl: newRegistry(),
		Handler: sprout.New(
			sprout.WithRegistries(
				std.NewRegistry(),
				strings.NewRegistry(),
				slices.NewRegistry(),
				maps.NewRegistry(),
				numeric.NewRegistry(),
			),
		),
	}
	reg.AddPathFS("mtr/pagination.html", paginationTemplate)
	return reg
}

// XXX: do we want Add{Base}(name, {reader/[]byte}) ?
func (r *Registry) addDt(name, path string, fs fs.FS, isBase bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dts = append(r.dts, deferredTemplate{name: name, path: path, isBase: isBase, fs: fs})
}

// AddBaseFS adds a base template on path from the fs under name
func (r *Registry) AddBaseFS(name, path string, fs fs.FS) {
	r.addDt(name, path, fs, true)
}

// AddPathFS adds path to the registry with the path as its name
func (r *Registry) AddPathFS(path string, fs fs.FS) {
	r.AddFS(path, path, fs)
}

// AddAllFS adds all paths in fs to the registry with paths as their name
func (r *Registry) AddAllFS(f fs.FS) {
	fs.WalkDir(f, ".", func(path string, d fs.DirEntry, err error) error {
		if gostrings.HasSuffix(path, ".html") {
			r.AddFS(path, path, f)
		}
		return nil
	})
}

// AddFS adds the named template from the given filesystem.
func (r *Registry) AddFS(name, path string, fs fs.FS) {
	r.addDt(name, path, fs, false)
}

func firstNonNil(ts []*template.Template) *template.Template {
	for _, t := range ts {
		if t.Tree == nil {
			continue
		}
		return t
	}
	return nil
}

// Build the registry templates. Caller should not add more templates
// after Build is called.
func (r *Registry) Build() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var errs []error

	fns := r.Handler.Build()
	slog.Debug("dts", "len", len(r.dts))

	for _, t := range r.dts {
		slog.Debug("building template", "name", t.name, "path", t.path, "isBase", t.isBase)
		tmpl, err := template.New(t.name).Funcs(fns).ParseFS(t.fs, t.path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		// a note about tmpl.Templates()[1] ...
		//
		// The {text,html}/template.Template type is meant to manage a set of templates,
		// which can refer to each other by name with {{block}} and {{template}} directives
		//
		// If you call template.New(name).Funcs(..).ParseFs(..), that "name" is given to
		// an empty template, and each template that matches the filter given to ParseFS
		// is added using its basename, so eg. "auth/login.html" gets added as "login.html"
		//
		// This is problematic;  it means that t.Execute() will fail because the "default"
		// template is empty!
		//
		// I don't want to use this template set functionality;  I have my own ideas about
		// how I want template nesting to work, so I want each template to be a single
		// entity. Since the empty "named" template is first in the set, we take the 2nd
		// element as the actual template from the FS we intended this to be.
		//
		// The path could potentially match multiple templates, but only one will be used.
		// This probably won't come up in practice.
		if len(tmpl.Templates()) != 2 {
			slog.Warn("found multiple templates", "name", t.name, "path", t.path, "isBase", t.isBase)
		}
		// take first template with a non-nil tree
		tmpl = firstNonNil(tmpl.Templates())
		if t.isBase {
			r.base.add(t.name, tmpl)
		} else {
			r.tmpl.add(t.name, tmpl)
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (r *Registry) getAny(name string) (*template.Template, error) {
	if c := r.tmpl.get(name); c != nil {
		return c, nil
	}
	if c := r.base.get(name); c != nil {
		return c, nil
	}
	return nil, fmt.Errorf("could not resolve template '%s'", name)
}

// Render the named template name to w using the provided context.
func (r *Registry) Render(w io.Writer, name string, ctx Ctx) error {
	c, err := r.getAny(name)
	if err != nil {
		return err
	}
	return c.Execute(w, ctx)
}

// RenderWithBase renders the template 'name' with the base template 'base' to the writer,
// using the provided context.  The base template template will receive the child template's
// content as 'body', which it should incorporate into its structure.
func (r *Registry) RenderWithBase(w io.Writer, base, name string, ctx Ctx) error {
	baseTpl := r.base.get(base)
	content := r.tmpl.get(name)

	if baseTpl == nil {
		return fmt.Errorf("could not find base template '%s'", base)
	}
	if content == nil {
		return fmt.Errorf("could not find template '%s'", name)
	}

	var s bytes.Buffer
	err := content.Execute(&s, ctx)
	if err != nil {
		return err
	}

	// make sure that this doesn't get escaped in the template
	ctx["body"] = template.HTML(s.String())
	if err != nil {
		return err
	}

	return baseTpl.Execute(w, ctx)
}

// Context adds this registry to the context.
func (r *Registry) Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, regKey{}, r)
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

package pages

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/mtr"
)

type Admin struct {
	db      db.DB
	BaseURL string
}

func NewPageAdmin(db db.DB) *Admin {
	return &Admin{db: db}
}
func (a *Admin) Bind(r chi.Router) {
	r.Get("/pages/", a.list)
	r.Get("/pages/{page:[0-9]+}", a.list)
	r.Get("/pages/add/", a.add)
	r.Get("/pages/edit/{id:\\d+}", a.edit)
	r.Get("/pages/delete/{id:\\d+}", a.del)

	r.Post("/pages/add/", a.add)
	r.Post("/pages/edit/{id:\\d+}", a.save)
}

// Render a blog admin panel.
func (a *Admin) Panels(r *http.Request) ([]string, error) {
	// published + unpublished panel
	// svc := NewPageService(a.db)

	var panels []string
	var pages []Page

	err := a.db.Select(&pages, `select * from page ORDER BY updated_at DESC LIMIT 5`)
	if err != nil {
		return nil, err
	}
	reg := mtr.RegistryFromContext(r.Context())

	var b bytes.Buffer

	err = reg.Render(&b, "pages/page-panel.html", mtr.Ctx{
		"fullUrl":   "pages/",
		"title":     "Pages",
		"renderAdd": true,
		"pages":     pages,
	})
	if err != nil {
		return nil, err
	}
	panels = append(panels, b.String())
	b.Reset()

	return panels, nil

}

func (a *Admin) list(w http.ResponseWriter, r *http.Request) {
	var count int
	if err := a.db.Get(&count, "SELECT count(*) FROM page;"); err != nil {
		app.Http500("getting count", w, err)
		return
	}

	const adminPageSize = 20
	paginator := mtr.NewPaginator(adminPageSize, count)
	page := paginator.Page(app.GetIntParam(r, "page", 1))

	var pages []Page
	err := a.db.Select(&pages, `SELECT * FROM page ORDER BY updated_at DESC LIMIT ? OFFSET ?`, adminPageSize, page.StartOffset)
	if err != nil {
		slog.Error("looking up pages", "error", err)
		app.Http404(w)
		return
	}

	reg := mtr.RegistryFromContext(r.Context())
	err = reg.RenderWithBase(w, "admin-base", "pages/admin/page-list.html", mtr.Ctx{
		"pages":      pages,
		"pagination": paginator.Render(reg, page),
	})

	if err != nil {
		slog.Error("rendering list", "err", err)
	}
}

func (a *Admin) del(w http.ResponseWriter, r *http.Request) {
	id := app.GetIntParam(r, "id", -1)
	if id < 0 {
		http.Redirect(w, r, "/admin/", http.StatusNotModified)
		return
	}

	svc := NewPageService(a.db)
	if err := svc.DeleteByID(id); err != nil {
		app.Http500("deleting", w, err)
	}

	http.Redirect(w, r, "/admin/", http.StatusFound)
}

func (a *Admin) edit(w http.ResponseWriter, r *http.Request) {
	svc := NewPageService(a.db)
	id := app.GetIntParam(r, "id", -1)
	p, err := svc.GetByID(id)
	if err != nil {
		slog.Info("Could not find page with id", "id", id)
		app.Http404(w)
		return
	}
	a.showEdit(w, r, p)
}

func (a *Admin) showEdit(w http.ResponseWriter, r *http.Request, p *Page) {
	reg := mtr.RegistryFromContext(r.Context())
	err := reg.RenderWithBase(w, "admin-base", "pages/page-edit.html", mtr.Ctx{
		"page": p,
	})

	if err != nil {
		app.Http500("rendering edit", w, err)
		return
	}
}

func (a *Admin) add(w http.ResponseWriter, r *http.Request) {
	// if post, insert a new page
	if r.Method == http.MethodGet {
		page := &Page{}
		// Pre-populate URL if provided as query parameter
		if url := r.URL.Query().Get("url"); url != "" {
			page.URL = url
		}
		a.showEdit(w, r, page)
		return

	}
	// save a new page
	r.ParseForm()
	p := &Page{
		Content: r.FormValue("content"),
		URL:     r.FormValue("url"),
	}
	svc := NewPageService(a.db)
	if err := svc.Insert(p); err != nil {
		app.Http500("inserting new page", w, err)
		return
	}

	// redirect to edit page
	http.Redirect(w, r, fmt.Sprintf("/admin/pages/edit/%d", p.ID), http.StatusFound)
}

func (a *Admin) save(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	id, _ := strconv.Atoi(r.FormValue("id"))

	svc := NewPageService(a.db)
	p, err := svc.GetByID(id)
	if err != nil {
		app.Http500("saving non-existant page", w, err)
		return
	}

	p.URL = r.FormValue("url")
	p.Content = r.FormValue("content")

	if err = svc.Save(p); err != nil {
		app.Http500("saving page", w, err)
		return
	}

	a.showEdit(w, r, p)
}

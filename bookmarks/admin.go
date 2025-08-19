package bookmarks

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/mtr"
)

const (
	panelListSize = 6
	adminPageSize = 20
)

type Admin struct {
	db      db.DB
	BaseURL string
}

func NewBookmarkAdmin(db db.DB) *Admin {
	return &Admin{db: db}
}

func (a *Admin) Bind(r chi.Router) {
	r.Get("/bookmarks/", a.bookmarkList)
	r.Get("/bookmarks/{page:[0-9]+}", a.bookmarkList)
	r.Get("/bookmarks/add/", a.add)
	r.Get("/bookmarks/edit/{id:[^/]+}", a.edit)

	r.Post("/bookmarks/add/", a.add)
	r.Post("/bookmarks/edit/{id:[^/]+}", a.save)
	r.Get("/bookmarks/delete/{id:[^/]+}", a.delete)
}

func (a *Admin) Panels(r *http.Request) ([]string, error) {
	serv := NewBookmarkService(a.db)
	bookmarks, err := serv.Select(fmt.Sprintf("ORDER BY created_at DESC LIMIT %d;", panelListSize))
	if err != nil {
		return nil, err
	}

	var panels []string

	reg := mtr.RegistryFromContext(r.Context())

	var b bytes.Buffer
	err = reg.Render(&b, "bookmarks/admin/bookmark-panel.html", mtr.Ctx{
		"fullUrl":   "bookmarks/",
		"title":     "Bookmarks",
		"renderAdd": true,
		"bookmarks": bookmarks,
	})
	if err != nil {
		return nil, err
	}
	panels = append(panels, b.String())

	return panels, nil
}

func (a *Admin) bookmarkList(w http.ResponseWriter, r *http.Request) {
	var count int
	if err := a.db.Get(&count, "SELECT count(*) FROM bookmark;"); err != nil {
		app.Http500("getting count", w, err)
		return
	}

	paginator := mtr.NewPaginator(adminPageSize, count)
	page := paginator.Page(app.GetIntParam(r, "page", 1))

	q := fmt.Sprintf(`ORDER BY created_at DESC LIMIT %d OFFSET %d`, adminPageSize, page.StartOffset)

	serv := NewBookmarkService(a.db)
	bookmarks, err := serv.Select(q)
	if err != nil {
		slog.Error("looking up bookmark", "error", err)
		app.Http404(w)
		return
	}

	reg := mtr.RegistryFromContext(r.Context())
	err = reg.RenderWithBase(w, "admin-base", "bookmarks/admin/bookmark-list.html", mtr.Ctx{
		"bookmarks":  bookmarks,
		"pagination": paginator.Render(reg, page),
	})

	if err != nil {
		slog.Error("rendering list", "err", err)
	}
}

func (a *Admin) edit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	serv := NewBookmarkService(a.db)

	b, err := serv.GetByID(id)
	if err != nil {
		app.Http500("getting by id", w, err)
		return
	}
	a.showEdit(w, r, b)
}

func (a *Admin) showEdit(w http.ResponseWriter, r *http.Request, b *Bookmark) {
	reg := mtr.RegistryFromContext(r.Context())
	err := reg.RenderWithBase(w, "admin-base", "bookmarks/admin/bookmark-edit.html", mtr.Ctx{
		"bookmark": b,
	})
	if err != nil {
		slog.Error("rendering edit", "err", err)
	}
}

func (a *Admin) save(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		app.Http500("parsing form", w, err)
		return
	}

	id := r.Form.Get("id")
	serv := NewBookmarkService(a.db)
	b, err := serv.GetByID(id)
	if err != nil {
		app.Http500("fetching bookmark", w, err)
		return
	}

	b.URL = r.Form.Get("url")
	b.Title = r.Form.Get("title")
	b.Description = r.Form.Get("description")

	formPub, _ := strconv.Atoi(r.Form.Get("published"))
	if b.Published != formPub {
		b.Published = formPub
		if b.Published == 0 {
			var t time.Time
			b.PublishedAt = t
		} else {
			b.PublishedAt = time.Now()
		}
	}

	if err := serv.Save(b); err != nil {
		app.Http500("saving bookmark", w, err)
		return
	}

	a.showEdit(w, r, b)
}

func (a *Admin) add(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	if r.Method == http.MethodGet {
		url := r.Form.Get("url")
		title := r.Form.Get("title")
		reg := mtr.RegistryFromContext(r.Context())
		err := reg.RenderWithBase(w, "admin-base", "bookmarks/admin/bookmark-edit.html", mtr.Ctx{
			"bookmark": Bookmark{URL: url, Title: title},
		})
		if err != nil {
			slog.Error("rendering add", "err", err)
		}
		return
	}

	var b Bookmark
	b.URL = r.Form.Get("url")
	b.Title = r.Form.Get("title")
	b.Description = r.Form.Get("description")

	b.Published, _ = strconv.Atoi(r.Form.Get("published"))
	if b.Published > 0 {
		b.PublishedAt = time.Now()
	}

	err := NewBookmarkService(a.db).Save(&b)
	if err != nil {
		app.Http500("saving bookmark", w, err)
		return
	}

	editUrl := fmt.Sprintf("../edit/%s", b.ID)
	http.Redirect(w, r, editUrl, http.StatusFound)
}

func (a *Admin) delete(w http.ResponseWriter, r *http.Request) {
	referer := r.Header.Get("Referer")
	if len(referer) == 0 {
		referer = "/admin/"
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		slog.Warn("deleted non-existent bookmark", "id", id)
		http.Redirect(w, r, referer, http.StatusFound)
		return
	}

	slog.Info("deleting bookmark", "id", id)
	_, err := a.db.Exec("DELETE FROM bookmark WHERE id=?", id)
	if err != nil {
		app.Http500("deleting bookmark", w, err)
		return
	}
	http.Redirect(w, r, referer, http.StatusFound)
}
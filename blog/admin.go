package blog

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

func NewBlogAdmin(db db.DB) *Admin {
	return &Admin{db: db}
}

func (a *Admin) Bind(r chi.Router) {
	r.Get("/unpublished/", a.unpublishedList)
	r.Get("/unpublished/{page:[0-9]+}", a.unpublishedList)

	r.Get("/posts/", a.postList)
	r.Get("/posts/{page:[0-9]+}", a.postList)
	r.Get("/posts/add/", a.add)
	r.Get("/posts/edit/{slug:[^/]+}", a.edit)

	r.Post("/posts/add/", a.add)
	r.Post("/posts/edit/{slug:[^/]+}", a.save)
	r.Get("/posts/delete/{id:\\d+}", a.delete)
	// r.Post("/posts/preview/", a.preview)
}

// Render a blog admin panel.
func (a *Admin) Panels(r *http.Request) ([]string, error) {
	// published + unpublished panel
	serv := NewPostService(a.db)
	published, err := serv.Select(fmt.Sprintf("WHERE published > 0 ORDER BY created_at DESC LIMIT %d;", panelListSize))
	if err != nil {
		return nil, err
	}
	unpublished, err := serv.Select(fmt.Sprintf("WHERE published = 0 ORDER BY updated_at DESC LIMIT %d;", panelListSize))
	if err != nil {
		return nil, err
	}

	var panels []string

	reg := mtr.RegistryFromContext(r.Context())

	var b bytes.Buffer
	err = reg.Render(&b, "blog/admin/post-panel.html", mtr.Ctx{
		"fullUrl":   "posts/",
		"title":     "Posts",
		"renderAdd": true,
		"posts":     published,
	})
	if err != nil {
		return nil, err
	}
	panels = append(panels, b.String())
	b.Reset()

	err = reg.Render(&b, "blog/admin/post-panel.html", mtr.Ctx{
		"fullUrl": "unpublished/",
		"title":   "Unpublished",
		"posts":   unpublished,
	})
	if err != nil {
		return nil, err
	}
	panels = append(panels, b.String())

	return panels, nil
}

func (a *Admin) unpublishedList(w http.ResponseWriter, r *http.Request) {
	var count int
	if err := a.db.Get(&count, "SELECT count(*) FROM post WHERE published = 0;"); err != nil {
		app.Http500("getting count", w, err)
		return
	}

	paginator := mtr.NewPaginator(adminPageSize, count)
	page := paginator.Page(app.GetIntParam(r, "page", 1))

	// select the posts for the page we're trying to render
	q := fmt.Sprintf(`WHERE published = 0 ORDER BY created_at DESC LIMIT %d OFFSET %d`, adminPageSize, page.StartOffset)

	serv := NewPostService(a.db)
	unpublished, err := serv.Select(q)

	reg := mtr.RegistryFromContext(r.Context())
	err = reg.RenderWithBase(w, "admin-base", "blog/admin/post-list.html", mtr.Ctx{
		"unpublished": true,
		"posts":       unpublished,
		"pagination":  paginator.Render(reg, page),
	})

	if err != nil {
		slog.Error("rendering list", "err", err)
	}

}

func (a *Admin) postList(w http.ResponseWriter, r *http.Request) {
	var count int
	if err := a.db.Get(&count, "SELECT count(*) FROM post WHERE published > 0;"); err != nil {
		app.Http500("getting count", w, err)
		return
	}

	paginator := mtr.NewPaginator(adminPageSize, count)
	page := paginator.Page(app.GetIntParam(r, "page", 1))

	// select the posts for the page we're trying to render
	q := fmt.Sprintf(`WHERE published > 0 ORDER BY created_at DESC LIMIT %d OFFSET %d`, adminPageSize, page.StartOffset)

	serv := NewPostService(a.db)
	posts, err := serv.Select(q)
	if err != nil {
		slog.Error("looking up post", "error", err)
		app.Http404(w)
		return
	}

	reg := mtr.RegistryFromContext(r.Context())
	err = reg.RenderWithBase(w, "admin-base", "blog/admin/post-list.html", mtr.Ctx{
		"posts":      posts,
		"pagination": paginator.Render(reg, page),
	})

	if err != nil {
		slog.Error("rendering list", "err", err)
	}
}

func (a *Admin) edit(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	serv := NewPostService(a.db)

	p, err := serv.GetSlug(slug)
	if err != nil {
		app.Http500("getting by slug", w, err)
		return
	}
	a.showEdit(w, r, p)
}

func (a *Admin) showEdit(w http.ResponseWriter, r *http.Request, p *Post) {
	reg := mtr.RegistryFromContext(r.Context())
	err := reg.RenderWithBase(w, "admin-base", "blog/admin/post-edit.html", mtr.Ctx{
		"post": p,
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

	id, err := strconv.Atoi(r.Form.Get("id"))
	if err != nil {
		app.Http500(fmt.Sprintf("invalid id sent: %s", r.Form.Get("id")), w, err)
		return
	}

	serv := NewPostService(a.db)
	p, err := serv.Get(id)
	if err != nil {
		app.Http500("fetching post", w, err)
		return
	}

	// update p and save
	p.Title = r.Form.Get("title")
	p.Slug = r.Form.Get("slug")
	p.Content = r.Form.Get("content")
	p.OgDescription = r.Form.Get("ogDescription")
	p.OgImage = r.Form.Get("ogImage")

	// if we're changing the published bit, set/unset the published at timestamp
	formPub, _ := strconv.Atoi(r.Form.Get("published"))
	if p.Published != formPub {
		p.Published = formPub
		if p.Published == 0 {
			var t time.Time
			p.PublishedAt = t
		} else {
			p.PublishedAt = time.Now()
		}
	}

	if err := serv.Save(p); err != nil {
		app.Http500("saving post", w, err)
		return
	}

	a.showEdit(w, r, p)

}

func (a *Admin) add(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	if r.Method == http.MethodGet {
		title := r.Form.Get("Title")
		reg := mtr.RegistryFromContext(r.Context())
		// redirect to edit page
		err := reg.RenderWithBase(w, "admin-base", "blog/admin/post-edit.html", mtr.Ctx{
			"post": Post{Title: title},
		})
		if err != nil {
			slog.Error("rendering add", "err", err)
		}
		return
	}

	var p Post
	p.Title = r.Form.Get("title")
	p.Slug = r.Form.Get("slug")
	p.Content = r.Form.Get("content")

	// if we're changing the published bit, set/unset the published at timestamp
	p.Published, _ = strconv.Atoi(r.Form.Get("published"))
	if p.Published > 0 {
		p.PublishedAt = time.Now()
	}

	err := NewPostService(a.db).Save(&p)
	if err != nil {
		app.Http500("saving post", w, err)
		return
	}

	editUrl := fmt.Sprintf("../edit/%s", p.Slug)
	http.Redirect(w, r, editUrl, http.StatusFound)

}

func (a *Admin) delete(w http.ResponseWriter, r *http.Request) {

	referer := r.Header.Get("Referer")
	if len(referer) == 0 {
		referer = "/admin/"
	}

	id := app.GetIntParam(r, "id", -1)
	if id <= 0 {
		slog.Warn("deleted non-existent post", "id", id)
		http.Redirect(w, r, referer, http.StatusFound)
		return
	}

	slog.Info("deleting post", "id", id)
	_, err := a.db.Exec("DELETE FROM post WHERE id=?", id)
	if err != nil {
		app.Http500("deleting post", w, err)
		return
	}
	http.Redirect(w, r, referer, http.StatusFound)

}

// *** Posts ***

/*

// List detail for published posts
func postList(page string) string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	num := app.PageNumber(page)

	paginator := app.NewPaginator(num, listPageSize)
	paginator.Link = "/admin/posts/"

	var posts []Post
	// do a search, if required, of title and content
	var err error
	var numObjects int

	if len(ctx.Params["Search"]) > 0 {
		term := M{"$regex": ctx.Params["Search"]}
		search := M{"published": 1, "$or": []M{
			M{"title": term},
			M{"content": term},
		}}
		err = db.Latest(&Post{}, search).Skip(paginator.Skip).Limit(listPageSize).All(&posts)
		numObjects, _ = db.Latest(&Post{}, search).Count()
	} else {
		err = db.Latest(&Post{}, M{"published": 1}).Skip(paginator.Skip).
			Limit(listPageSize).All(&posts)
		numObjects, _ = db.Find(&Post{}, M{"published": 1}).Count()
	}

	if err != nil {
		fmt.Println(err)
	}

	return adminBase.Render("blog/admin/post-list.mandira", M{
		"Posts": posts, "Pagination": paginator.Render(numObjects)})

}

func postAdd() string {
	if app.RequireAuthentication(ctx) {
		return ""
	}

	if ctx.Request.Method == "GET" {
		return adminBase.Render("blog/admin/posts-edit.mandira",
			ctx.Params, M{"Published": 0, "IsPublished": false})
	}

	post := new(Post)
	post.FromParams(ctx.Params)
	_, err := db.Upsert(post)
	if err != nil {
		fmt.Println(err)
	}
	ctx.Redirect(302, "/admin/")
	return ""
	//ctx.Redirect(302, "/admin/posts/edit/" + post.Slug + "/")
}

func postEdit(slug string) string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	var post *Post
	err := db.Find(post, M{"slug": slug}).One(&post)
	if err != nil {
		fmt.Println(err)
		ctx.Redirect(302, "/admin/")
		return ""
	}
	if len(ctx.Params) > 1 {
		post.FromParams(ctx.Params)
		db.Upsert(post)
	}

	return adminBase.Render("blog/admin/posts-edit.mandira", post, M{
		"IsPublished": post.Published == 1,
		"IdHex":       post.Id.Hex()})
}

func postPreview() string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	var post = new(Post)
	post.FromParams(ctx.Params)
	return RenderPost(post)
}

func postDelete(slug string) string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	db.Cursor(&Post{}).Remove(M{"slug": slug})
	referer := ctx.Request.Header.Get("referer")
	if len(referer) == 0 {
		referer = "/admin/"
	}
	ctx.Redirect(302, referer)
	return ""
}

// *** Pages ***

func pageAdd() string {
	if app.RequireAuthentication(ctx) {
		return ""
	}

	if ctx.Request.Method == "GET" {
		ctx.Params["Url"] = strings.TrimLeft(ctx.Params["Url"], "/")
		return adminBase.Render("blog/admin/pages-edit.mandira", ctx.Params)
	}

	var page = new(Page)
	page.FromParams(ctx.Params)
	db.Upsert(page)
	ctx.Redirect(302, "/admin/")
	return ""
	//ctx.Redirect(302, "/admin/posts/edit/" + post.Slug + "/")
}

func pageEdit(url string) string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	var page *Page
	err := db.Find(page, M{"url": url}).One(&page)
	if err != nil {
		fmt.Println(err)
		ctx.Redirect(302, "/admin/")
		return ""
	}
	if len(ctx.Params) > 1 {
		page.FromParams(ctx.Params)
		db.Upsert(page)
	}
	return adminBase.Render("blog/admin/pages-edit.mandira", page)
}

func pagePreview() string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	var page = new(Page)
	page.FromParams(ctx.Params)
	return template.RenderMarkdown(page.Content)
}

func pageDelete(url string) string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	db.Cursor(&Page{}).Remove(M{"url": url})
	referer := ctx.Request.Header.Get("referer")
	if len(referer) == 0 {
		referer = "/admin/"
	}
	ctx.Redirect(302, referer)
	return ""
}

func pageList(page string) string {
	if app.RequireAuthentication(ctx) {
		return ""
	}

	num := app.PageNumber(page)
	paginator := app.NewPaginator(num, listPageSize)
	paginator.Link = "/admin/pages/"
	sort := "url"

	var p *Page
	var pages []Page
	// do a search, if required, of title and content
	var err error
	var numObjects int

	if len(ctx.Params["Search"]) > 0 {
		term := M{"$regex": ctx.Params["Search"]}
		search := M{"$or": []M{M{"url": term}, M{"content": term}}}
		err = db.Find(p, search).Sort(sort).Skip(paginator.Skip).Limit(listPageSize).All(&pages)
		numObjects, _ = db.Find(p, search).Count()
	} else {
		err = db.Find(p, nil).Sort(sort).Skip(paginator.Skip).Limit(listPageSize).All(&pages)
		numObjects, _ = db.Cursor(p).Count()
	}

	if err != nil {
		fmt.Println(err)
	}

	return adminBase.Render("blog/admin/page-list.mandira", M{
		"Pages": pages, "Pagination": paginator.Render(numObjects)})
}

// *** Panels ***

type PostPanel struct{}

func (pp *PostPanel) Render() string {
	var posts []Post
	db.Latest(&Post{}, M{"published": 1}).Limit(indexListSize).All(&posts)
	return template.Render("blog/admin/posts-panel.mandira", M{
		"posts": posts,
	})
}

type UnpublishedPanel struct{}

func (up *UnpublishedPanel) Render() string {
	var posts []Post
	db.Latest(&Post{}, M{"published": 0}).Limit(indexListSize).All(&posts)
	return template.Render("blog/admin/unpublished-panel.mandira", M{
		"posts": posts,
	})
}

type PagesPanel struct{}

func (pp *PagesPanel) Render() string {
	var pages []Page
	db.Find(&Page{}, nil).Limit(indexListSize).All(&pages)
	return template.Render("blog/admin/pages-panel.mandira", M{
		"pages": pages,
	})
}

*/

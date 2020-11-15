package blog

import (
	"github.com/gorilla/mux"
	"github.com/jmoiron/monet/sunrise"
	"github.com/jmoiron/monet/template"
	"github.com/jmoiron/sqlx"
	"labix.org/v2/mgo/bson"
)

type BlogAdmin struct {
	db *sqlx.DB
}

func NewBlogAdmin(db *sqlx.DB) {
	return &BlogAdmin{db: db}
}

func (b *BlogAdmin) Attach(r *mux.Router, path string) error {
	prefix := r.PathPrefix(path)

	var (
		get     = prefix.Methods("GET").Subrouter()
		post    = prefix.Methods("POST").Subrouter()
		getPost = prefix.Methods("GET", "POST").Subrouter()
	)

	get.HandleFunc("/unpublished/", b.unpublishedList)
	get.HandleFunc("/unpublished/{page:[0-9]+}", b.unpublishedList)
	get.HandleFunc("/posts/", b.postList)
	get.HandleFunc("/posts/{page:[0-9]+}", b.postList)

	getPost.HandleFunc("/posts/edit/{slug:[^/]+}", b.edit)
	getPost.HandleFunc("/posts/add/", b.add)

	post.HandleFunc("/posts/delete/{id:[^/]+}", b.delete)
	post.HandleFunc("/posts/preview/", b.preview)

	return nil
}

// Panel renders a blog admin panel.
func (b *BlogAdmin) Panel() ([]byte, error) {

}

var _ sunrise.Admin = &BlogAdmin{}

type M bson.M

var (
	adminBase     = template.Base{Path: "admin/base.mandira"}
	listPageSize  = 20
	indexListSize = 6
)

func AttachAdmin(url string) {
	// pages
	/*
		app.GetPost(url+"pages/add/", pageAdd)
		app.GetPost(url+"pages/edit/(.*)", pageEdit)
		web.Post(url+"pages/preview/", pagePreview)
		web.Get(url+"pages/delete/(.*)", pageDelete)
		web.Get(url+"pages/(\\d+)?", pageList)
	*/
}

// *** Posts ***

/*
// List detail for unpublished posts
func unpublishedList(page string) string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	num := app.PageNumber(page)

	paginator := app.NewPaginator(num, listPageSize)
	paginator.Link = "/admin/unpublished/"

	var posts []Post
	latest := db.Latest(&Post{}, M{"published": 0})
	latest.Limit(listPageSize).All(&posts)

	numObjects, _ := latest.Count()
	return adminBase.Render("blog/admin/post-list.mandira", M{
		"Posts":       posts,
		"Pagination":  paginator.Render(numObjects),
		"Unpublished": true,
	})
}

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

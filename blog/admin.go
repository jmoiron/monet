package blog

import (
	"fmt"
	"github.com/hoisie/web"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/template"
	"labix.org/v2/mgo/bson"
	"strings"
)

type M bson.M

var (
	adminBase     = template.Base{Path: "admin/base.mustache"}
	listPageSize  = 20
	indexListSize = 6
)

func AttachAdmin(url string) {
	web.Get(url+"unpublished/(\\d+)?", unpublishedList)
	web.Get(url+"posts/(\\d+)?", postList)
	web.Get(url+"posts/edit/(.*)", postEdit)
	web.Post(url+"posts/edit/(.*)", postEdit)
	web.Get(url+"posts/delete/(.*)", postDelete)
	web.Get(url+"posts/add/", postAddGet)
	web.Post(url+"posts/add/", postAddPost)
	web.Post(url+"posts/preview/", postPreview)
	// pages
	web.Get(url+"pages/add/", pageAddGet)
	web.Post(url+"pages/add/", pageAddPost)
	web.Get(url+"pages/edit/(.*)", pageEdit)
	web.Post(url+"pages/edit/(.*)", pageEdit)
	web.Post(url+"pages/preview/", pagePreview)
	web.Get(url+"pages/delete/(.*)", pageDelete)
	web.Get(url+"pages/(\\d+)?", pageList)
}

// *** Posts ***

// List detail for unpublished posts
func unpublishedList(ctx *web.Context, page string) string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	num := app.PageNumber(page)

	paginator := app.NewPaginator(num, listPageSize)
	paginator.Link = "/admin/unpublished/"

	var posts []Post
	latest := db.Latest(&Post{}, M{"published": 0})
	latest.Limit(listPageSize).Iter().All(&posts)

	numObjects, _ := latest.Count()
	return adminBase.Render("admin/post-list.mustache", M{
		"Posts":       posts,
		"Pagination":  paginator.Render(numObjects),
		"Unpublished": true,
	})
}

// List detail for published posts
func postList(ctx *web.Context, page string) string {
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

	return adminBase.Render("admin/post-list.mustache", M{
		"Posts": posts, "Pagination": paginator.Render(numObjects)})

}

func postAddGet(ctx *web.Context) string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	return adminBase.Render("admin/posts-edit.mustache",
		ctx.Params, M{"Published": 0, "IsPublished": false})
}

func postAddPost(ctx *web.Context) string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	post := new(Post)
	post.FromParams(ctx.Params)
	db.Upsert(post)
	ctx.Redirect(302, "/admin/")
	return ""
	//ctx.Redirect(302, "/admin/posts/edit/" + post.Slug + "/")
}

func postEdit(ctx *web.Context, slug string) string {
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

	return adminBase.Render("admin/posts-edit.mustache", post, M{
		"IsPublished": post.Published == 1,
		"IdHex":       post.Id.Hex()})
}

func postPreview(ctx *web.Context) string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	var post = new(Post)
	post.FromParams(ctx.Params)
	/* not sure the ettiquite here, RenderPost is defined in app.go */
	return RenderPost(post)
}

func postDelete(ctx *web.Context, slug string) string {
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

func pageAddGet(ctx *web.Context) string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	ctx.Params["Url"] = strings.TrimLeft(ctx.Params["Url"], "/")
	return adminBase.Render("admin/pages-edit.mustache", ctx.Params)
}

func pageAddPost(ctx *web.Context) string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	var page = new(Page)
	page.FromParams(ctx.Params)
	db.Upsert(page)
	ctx.Redirect(302, "/admin/")
	return ""
	//ctx.Redirect(302, "/admin/posts/edit/" + post.Slug + "/")
}

func pageEdit(ctx *web.Context, url string) string {
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
	return adminBase.Render("admin/pages-edit.mustache", page)
}

func pagePreview(ctx *web.Context) string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	var page = new(Page)
	page.FromParams(ctx.Params)
	return template.RenderMarkdown(page.Content)
}

func pageDelete(ctx *web.Context, url string) string {
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

func pageList(ctx *web.Context, page string) string {
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
		err = db.Find(p, nil).Sort(sort).Skip(paginator.Skip).Limit(listPageSize).Iter().All(&pages)
		numObjects, _ = db.Cursor(p).Count()
	}

	if err != nil {
		fmt.Println(err)
	}

	return adminBase.Render("admin/page-list.mustache", M{
		"Pages": pages, "Pagination": paginator.Render(numObjects)})
}

// *** Panels ***

type PostPanel struct{}

func (pp *PostPanel) Render() string {
	var posts []Post
	db.Latest(&Post{}, M{"published": 1}).Limit(indexListSize).All(&posts)
	return template.Render("blog/posts-panel.mustache", M{
		"posts": posts,
	})
}

type UnpublishedPanel struct{}

func (up *UnpublishedPanel) Render() string {
	var posts []Post
	db.Latest(&Post{}, M{"published": 0}).Limit(indexListSize).All(&posts)
	return template.Render("blog/unpublished-panel.mustache", M{
		"posts": posts,
	})
}

type PagesPanel struct{}

func (pp *PagesPanel) Render() string {
	var pages []Page
	db.Find(&Page{}, nil).Limit(indexListSize).All(&pages)
	return template.Render("blog/pages-panel.mustache", M{
		"pages": pages,
	})
}

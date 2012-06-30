package app

import (
    "fmt"
    "strings"
    "strconv"
    "monet/db"
    "github.com/hoisie/web"
    "monet/template"
    "monet/conf"
    "code.google.com/p/gorilla/sessions"
)

var listPageSize = 15
var indexListSize = 6

var adminBase = template.Base {Path: "admin/base.mustache"}
var store = sessions.NewCookieStore([]byte(conf.Config.SessionSecret))

func AttachAdmin(url string) {
    // auth
    web.Get(url + "login/", login)
    web.Post(url + "login/", login)
    web.Get(url + "logout/", logout)
    // users
    /*  too much unnecessary work? 
    web.Get(url + "users/", userList)
    web.Get(url + "users/edit/(.*)", userEdit)
    web.Get(url + "users/delete/(.*)", userDelete)
    web.Get(url + "users/add/", userAdd)
    web.Post(url + "users/add/", userAddPost)
    */
    // posts
    web.Get(url + "unpublished/(\\d+)?", unpublishedList)
    web.Get(url + "posts/(\\d+)?", postList)
    web.Get(url + "posts/edit/(.*)", postEdit)
    web.Post(url + "posts/edit/(.*)", postEdit)
    web.Get(url + "posts/delete/(.*)", postDelete)
    web.Get(url + "posts/add/", postAdd)
    web.Post(url + "posts/add/", postAddPost)
    web.Post(url + "posts/preview/", postPreview)
    // pages
    web.Get(url + "pages/add/", pageAdd)
    web.Post(url + "pages/add/", pageAddPost)
    web.Get(url + "pages/edit/(.*)", pageEdit)
    web.Post(url + "pages/edit/(.*)", pageEdit)
    web.Post(url + "pages/preview/", pagePreview)
    web.Get(url + "pages/delete/(.*)", pageDelete)
    web.Get(url + "pages/(\\d+)?", pageList)

    web.Get(url, adminIndex)
}

func requireAuthentication(ctx *web.Context) bool {
    session,_ := store.Get(ctx.Request, "monet-session")

    if session.Values["authenticated"] != true {
        ctx.Redirect(302, "/admin/login/")
        return true
    }
    return false
}

// views

func login(ctx *web.Context) string {
    if ctx.Params != nil {
        p := ctx.Params
        if db.Users().Validate(p["username"], p["password"]) {
            session,_ := store.Get(ctx.Request, "monet-session")
            session.Values["authenticated"] = true
            session.Save(ctx.Request, ctx.ResponseWriter)
            ctx.Redirect(302, "/admin/")
        }
    }
    return adminBase.Render("admin/login.mustache", ctx.Params, dict{"login":true})
}

func logout(ctx *web.Context) string {
    session,_ := store.Get(ctx.Request, "monet-session")
    session.Values["authenticated"] = false
    session.Save(ctx.Request, ctx.ResponseWriter)
    ctx.Redirect(302, "/admin/login/")
    return ""
}

func adminIndex(ctx *web.Context) string {
    if requireAuthentication(ctx) { return "" }
    var posts []db.Post
    var unpublished []db.Post
    var pages []db.Page
    db.Posts().Latest(dict{"published":1}).Limit(indexListSize).Iter().All(&posts)
    db.Posts().Latest(dict{"published":0}).Limit(indexListSize).Iter().All(&unpublished)
    db.Pages().C.Find(nil).Limit(indexListSize).Iter().All(&pages)

    return adminBase.Render("admin/index.mustache", dict{
        "posts": posts, "unpublished": unpublished, "pages": pages})
}

func postEdit(ctx *web.Context, slug string) string {
    if requireAuthentication(ctx) { return "" }
    var post *db.Post
    err := db.Posts().C.Find(dict{"slug":slug}).One(&post)
    if err != nil {
        fmt.Println(err)
        ctx.Redirect(302, "/admin/")
        return ""
    }
    if len(ctx.Params) > 1 {
        post.FromParams(ctx.Params)
        post.Update()
    }

    return adminBase.Render("admin/posts-edit.mustache", post, dict{
        "IsPublished": post.Published == 1,
        "IdHex": post.Id.Hex()})
}

func postList(ctx *web.Context, page string) string {
    if requireAuthentication(ctx) { return "" }
    pageNum := 1
    if len(page) != 0 { pageNum,_ = strconv.Atoi(page) }

    n := listPageSize
    paginator := NewPaginator(pageNum, n)
    paginator.Link = "/admin/posts/"
    cursor := db.Posts()

    var posts []db.Post
    // do a search, if required, of title and content
    var err error
    var numObjects int

    if len(ctx.Params["Search"]) > 0 {
        term := dict{"$regex": ctx.Params["Search"]}
        search := dict{"published":1, "$or": []dict{dict{"title":term}, dict{"content":term}}}
        err = cursor.Latest(search).Skip(paginator.Skip).Limit(n).All(&posts)
        numObjects,_ = cursor.Latest(search).Count()
    } else {
        err = cursor.Latest(dict{"published":1}).Skip(paginator.Skip).
            Limit(n).Iter().All(&posts)
        numObjects,_ = cursor.C.Find(dict{"published":1}).Count()
    }

    if err != nil {
        fmt.Println(err)
    }

    return adminBase.Render("admin/post-list.mustache", dict{
        "Posts": posts, "Pagination": paginator.Render(numObjects)})
}

func unpublishedList(ctx *web.Context, page string) string {
    if requireAuthentication(ctx) { return "" }
    pageNum := 1
    if len(page) != 0 { pageNum,_ = strconv.Atoi(page) }

    paginator := NewPaginator(pageNum, listPageSize)
    paginator.Link = "/admin/unpublished/"
    cursor := db.Posts()
    var posts []db.Post
    latest := cursor.Latest(dict{"published":0})
    latest.Limit(listPageSize).Iter().All(&posts)
    numObjects,_ := latest.Count()
    return adminBase.Render("admin/post-list.mustache", dict{
        "Posts": posts, "Pagination": paginator.Render(numObjects),
        "Unpublished": true})

}

func postAdd(ctx *web.Context) string {
    if requireAuthentication(ctx) { return "" }
    return adminBase.Render("admin/posts-edit.mustache", ctx.Params,
        dict{"Published": 0, "IsPublished": false})
}

func postAddPost(ctx *web.Context) string {
    if requireAuthentication(ctx) { return "" }
    post := new(db.Post)
    post.FromParams(ctx.Params)
    post.Update()
    ctx.Redirect(302, "/admin/")
    return ""
    //ctx.Redirect(302, "/admin/posts/edit/" + post.Slug + "/")
}

func postPreview(ctx *web.Context) string {
    if requireAuthentication(ctx) { return "" }
    var post = new(db.Post)
    post.FromParams(ctx.Params)
    /* not sure the ettiquite here, RenderPost is defined in app.go */
    return RenderPost(post)
}

func postDelete(ctx *web.Context, slug string) string {
    if requireAuthentication(ctx) { return "" }
    db.Posts().C.Remove(dict{"slug": slug})
    referer := ctx.Request.Header.Get("referer")
    if len(referer) == 0 {
        referer = "/admin/"
    }
    ctx.Redirect(302, referer)
    return ""
}

func pageAdd(ctx *web.Context) string {
    if requireAuthentication(ctx) { return "" }
    ctx.Params["Url"] = strings.TrimLeft(ctx.Params["Url"], "/")
    return adminBase.Render("admin/pages-edit.mustache", ctx.Params)
}

func pageAddPost(ctx *web.Context) string {
    if requireAuthentication(ctx) { return "" }
    var page = new(db.Page)
    page.FromParams(ctx.Params)
    page.Update()
    ctx.Redirect(302, "/admin/")
    return ""
    //ctx.Redirect(302, "/admin/posts/edit/" + post.Slug + "/")
}

func pageEdit(ctx *web.Context, url string) string {
    if requireAuthentication(ctx) { return "" }
    var page *db.Page
    err := db.Pages().C.Find(dict{"url": url}).One(&page)
    if err != nil {
        fmt.Println(err)
        ctx.Redirect(302, "/admin/")
        return ""
    }
    if len(ctx.Params) > 1 {
        page.FromParams(ctx.Params)
        page.Update()
    }
    return adminBase.Render("admin/pages-edit.mustache", page)
}

func pagePreview(ctx *web.Context) string {
    if requireAuthentication(ctx) { return "" }
    var page = new(db.Page)
    page.FromParams(ctx.Params)
    return template.RenderMarkdown(page.Content)
}

func pageDelete(ctx *web.Context, url string) string {
    if requireAuthentication(ctx) { return "" }
    db.Pages().C.Remove(dict{"url": url})
    referer := ctx.Request.Header.Get("referer")
    if len(referer) == 0 {
        referer = "/admin/"
    }
    ctx.Redirect(302, referer)
    return ""
}

func pageList(ctx *web.Context, page string) string {
    if requireAuthentication(ctx) { return "" }
    pageNum := 1
    if len(page) != 0 { pageNum,_ = strconv.Atoi(page) }

    n := listPageSize
    paginator := NewPaginator(pageNum, n)
    paginator.Link = "/admin/pages/"
    cursor := db.Pages().C
    sort := dict{"url":1}

    var pages []db.Page
    // do a search, if required, of title and content
    var err error
    var numObjects int

    if len(ctx.Params["Search"]) > 0 {
        term := dict{"$regex": ctx.Params["Search"]}
        search := dict{"$or": []dict{dict{"url":term}, dict{"content":term}}}
        err = cursor.Find(search).Sort(sort).Skip(paginator.Skip).Limit(n).All(&pages)
        numObjects,_ = cursor.Find(search).Count()
    } else {
        err = cursor.Find(nil).Sort(sort).Skip(paginator.Skip).Limit(n).Iter().All(&pages)
        numObjects,_ = cursor.Count()
    }

    if err != nil {
        fmt.Println(err)
    }

    return adminBase.Render("admin/page-list.mustache", dict{
        "Pages": pages, "Pagination": paginator.Render(numObjects)})
}


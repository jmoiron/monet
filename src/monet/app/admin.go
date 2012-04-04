package app

import (
    "fmt"
    "monet/db"
    "github.com/hoisie/web.go"
    "monet/template"
    "monet/conf"
    "code.google.com/p/gorilla/sessions"
)

var adminBase = template.Base {Path: "admin/base.mustache"}
var store = sessions.NewCookieStore([]byte(conf.Config.SessionSecret))

func AttachAdmin(url string) {
    // auth
    web.Get(url + "login/", login)
    web.Post(url + "login/", login)
    web.Get(url + "logout/", logout)
    // posts
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
    // notes
    web.Get(url + "notes/edit/", noteEdit)
    // web.Get(url + "/users/", usersList)
    // web.Get(url + "/users/add/", usersAdd)
    // web.Get(url + "/users/edit/", usersEdit)

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
    if requireAuthentication(ctx) {
        return ""
    }
    var posts []db.Post
    var unpublished []db.Post
    var pages []db.Page
    db.Posts().Latest(dict{"published":1}).Limit(10).Iter().All(&posts)
    db.Posts().Latest(dict{"published":0}).Limit(10).Iter().All(&unpublished)
    db.Pages().C.Find(nil).Limit(10).Iter().All(&pages)

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

func postAdd(ctx *web.Context) string {
    if requireAuthentication(ctx) {
        return ""
    }
    return adminBase.Render("admin/posts-edit.mustache", ctx.Params,
        dict{"Published": 0, "IsPublished": false})
}

func postAddPost(ctx *web.Context) string {
    post := new(db.Post)
    post.FromParams(ctx.Params)
    post.Update()
    ctx.Redirect(302, "/admin/")
    return ""
    //ctx.Redirect(302, "/admin/posts/edit/" + post.Slug + "/")
}

func postPreview(ctx *web.Context) string {
    var post = new(db.Post)
    post.FromParams(ctx.Params)
    /* not sure the ettiquite here, RenderPost is defined in app.go */
    return RenderPost(post)
}

func postDelete(ctx *web.Context) string {
    return ""
}

func pageAdd(ctx *web.Context) string {
    if requireAuthentication(ctx) { return "" }
    return adminBase.Render("admin/pages-edit.mustache", ctx.Params)
}

func pageAddPost(ctx *web.Context) string {
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
    var page = new(db.Page)
    page.FromParams(ctx.Params)
    return template.RenderMarkdown(page.Content)
}

func noteEdit(ctx *web.Context) string {
    return ""
}

func noteAdd(ctx *web.Context) string {
    return ""
}



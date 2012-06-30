package app

import (
    "fmt"
    "strconv"
    "github.com/hoisie/web"
    "monet/template"
    "monet/db"
)

type dict map[string]interface{}

var base = template.Base {Path: "base.mustache"}

func Attach(url string) {
    web.Get(url + "blog/page/(\\d+)", blogPage)
    web.Get(url + "blog/([^/]+)/", blogDetail)
    web.Get(url + "blog/", blogIndex)
    web.Get(url + "([^/]*)", index)
    web.Get(url + "(.*)", page)
}

// helpers

func RenderPost(post *db.Post) string {
    if len(post.ContentRendered) == 0 {
        post.Update()
    }
    return template.Render("post.mustache", post)
}

// views

func page(ctx *web.Context, url string) string {
    p := db.Pages().Get(url)
    if p == nil {
        ctx.Abort(404, "Page not found")
        return ""
    }
    return template.Render("base.mustache", dict{"body": p.ContentRendered})
}

func index(s string) string {
    var post *db.Post
    var posts []db.Post
    var entries []db.StreamEntry

    err := db.Posts().Latest(dict{"published":1}).Limit(7).All(&posts)
    if err != nil {
        fmt.Println(err)
    }
    err = db.Entries().Latest(nil).Limit(4).All(&entries)
    if err != nil {
        fmt.Println(err)
    }

    post = &posts[0]
    return base.Render("index.mustache", dict{
        "Post": RenderPost(post),
        "Posts": posts[1:],
        "Entries": entries})
}

func blogIndex(ctx *web.Context) string {
    return blogPage(ctx, "1")
}

func blogPage(ctx *web.Context, page string) string {
    pn,_ := strconv.Atoi(page)
    perPage := 15
    paginator := NewPaginator(pn, perPage)
    paginator.Link = "/blog/page/"
    cursor := db.Posts()

    var posts []db.Post
    // do a search, if required, of title and content
    var err error
    var numObjects int

    if len(ctx.Params["Search"]) > 0 {
        term := dict{"$regex": ctx.Params["Search"]}
        search := dict{"published":1, "$or": []dict{dict{"title":term}, dict{"content":term}}}
        err = cursor.Latest(search).Skip(paginator.Skip).Limit(perPage).All(&posts)
        numObjects,_ = cursor.Latest(search).Count()
    } else {
        err = cursor.Latest(dict{"published":1}).Skip(paginator.Skip).
            Limit(perPage).Iter().All(&posts)
        numObjects,_ = cursor.C.Find(dict{"published":1}).Count()
    }

    if err != nil {
        fmt.Println(err)
    }

    return base.Render("blog-index.mustache", dict{
        "Posts": posts, "Pagination": paginator.Render(numObjects)}, ctx.Params)
}

func blogDetail(ctx *web.Context, slug string) string {
    var post = new(db.Post)
    err := db.Posts().C.Find(dict{"slug": slug}).One(&post)
    if err != nil {
        fmt.Println(err)
        ctx.Abort(404, "Page not found")
        return ""
    }
    return template.Render("base.mustache", dict{"body": RenderPost(post)})
}


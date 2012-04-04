package app

import (
    "fmt"
    "strconv"
    "github.com/hoisie/web.go"
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
    post := db.Posts().LatestPost()
    return template.Render("base.mustache", dict{"body": RenderPost(post)})
}

func blogIndex() string {
    return blogPage("1")
}

func blogPage(page string) string {
    pn,_ := strconv.Atoi(page)
    perPage := 10
    paginator := NewPaginator(pn, perPage)
    paginator.Link = "/blog/page/"

    var posts []db.Post
    err := db.Posts().Latest(dict{"published":1}).Skip(paginator.Skip).Limit(10).Iter().All(&posts)
    if err != nil {
        fmt.Println(err)
    }
    numObjects,_ := db.Posts().C.Count()
    rendered := []dict{}
    for _,p := range posts {
        rendered = append(rendered, dict{"Body": RenderPost(&p)})
    }
    return base.Render("post-list.mustache", dict{
        "Posts": rendered, "Pagination": paginator.Render(numObjects)})
}

func blogDetail(ctx *web.Context, slug string) string {
    var post = new(db.Post)
    fmt.Println(slug)
    err := db.Posts().C.Find(dict{"slug": slug}).One(&post)
    if err != nil {
        fmt.Println(err)
        ctx.Abort(404, "Page not found")
        return ""
    }
    return template.Render("base.mustache", dict{"body": RenderPost(post)})
}

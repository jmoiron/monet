package app

import (
    "fmt"
    "time"
    "github.com/hoisie/web.go"
    "monet/template"
    "monet/db"
)

type dict map[string]interface{}

var base = template.Base {Path: "base.mustache"}

func Attach(url string) {
    web.Get(url + "blog/([^/]+)/", blogDetail)
    web.Get(url + "blog/", blogIndex)
    web.Get(url + "([^/]*)", index)
    web.Get(url + "(.*)", page)
}

// helpers

func FmtTimestamp(ts uint64) string {
    ut := time.Unix(int64(ts), 0)
    return ut.Format("Jan _2")
}

func RenderPost(post *db.Post) string {
    if post.Content != "" && post.ContentRendered == "" {
        post.Update()
    }
    return template.Render("post.mustache", post, dict{
        "NaturalTime": FmtTimestamp(post.Timestamp)})
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
    var posts []db.Post
    err := db.Posts().Latest(dict{"published":1}).Limit(10).Iter().All(&posts)
    if err != nil {
        fmt.Println(err)
    }
    rendered := []dict{}
    for _,p := range posts {
        rendered = append(rendered, dict{"Body": RenderPost(&p)})
    }
    return base.Render("post-list.mustache", dict{"Posts": rendered})
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

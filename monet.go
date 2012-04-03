package main

import (
    "github.com/hoisie/web.go"
    "monet/template"
    "monet/conf"
    "monet/app"
    "monet/db"
    "fmt"
)

func main() {
    fmt.Printf("Using config %s\n", conf.Path)
    template.Init(conf.Config.TemplatePaths)
    fmt.Printf(conf.Config.String())
    // finished config
    db.Connect()

    app.AttachAdmin("/admin/")
    app.Attach("/")
    web.Run(conf.Config.HostString())
}

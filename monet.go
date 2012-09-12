package main

import (
	"fmt"
	"github.com/hoisie/web"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/conf"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/gallery"
	"github.com/jmoiron/monet/template"
)

func main() {
	fmt.Printf("Using config %s\n", conf.Path)
	template.Init(conf.Config.TemplatePaths)
	fmt.Printf(conf.Config.String())
	web.Config.StaticDir = conf.Config.StaticPath
	// finished config
	db.Connect()

	gallery.AttachAdmin("/admin/")
	app.AttachAdmin("/admin/")
	app.Attach("/")
	web.Run(conf.Config.HostString())
}

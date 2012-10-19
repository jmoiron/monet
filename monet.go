package main

import (
	"fmt"
	"github.com/hoisie/web"
	"github.com/jmoiron/monet/blog"
	"github.com/jmoiron/monet/conf"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/template"
	"reflect"
	// apps
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/gallery"
)

func main() {
	fmt.Printf("Using config %s\n", conf.Path)
	fmt.Printf("Using models:\n")
	for _, m := range db.Models {
		t := reflect.TypeOf(m)
		fmt.Printf("    %s\n", fmt.Sprintf("%s", t)[1:])
	}

	template.Init(conf.Config.TemplatePaths)
	fmt.Printf(conf.Config.String())
	web.Config.StaticDir = conf.Config.StaticPath

	db.Connect(conf.Config.DbHostString(), conf.Config.DbName)
	db.RegisterAllIndexes()

	blog.AttachAdmin("/admin/")
	blog.Attach("/")

	app.AttachAdmin("/admin/")
	app.Attach("/")

	gallery.AttachAdmin("/admin/")
	gallery.Attach("/")

	web.Get("/([^/]*)", blog.Index)
	web.Get("/(.*)", blog.Flatpage)

	app.AddPanel(&blog.PostPanel{})
	app.AddPanel(&blog.UnpublishedPanel{})
	app.AddPanel(&blog.PagesPanel{})
	app.AddPanel(&gallery.GalleryPanel{})

	web.Run(conf.Config.HostString())
}

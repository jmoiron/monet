package main

import (
	"fmt"
	"github.com/hoisie/web"
	"monet/app"
	"monet/conf"
	"monet/db"
	"monet/template"
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

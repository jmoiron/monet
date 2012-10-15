package gallery

import (
	"errors"
	"fmt"
	"github.com/hoisie/web"
	//"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/conf"
	"github.com/jmoiron/monet/template"
)

type dict map[string]interface{}

var adminBase = template.Base{Path: "admin/base.mustache"}
var galleryBase = template.Base{Path: "gallery/base.mustache"}

func AttachAdmin(url string) {
	// auth
	web.Get(url+"gallery/", galleryList)
}

type GalleryConfig struct {
	conf   map[string]string
	Type   string
	UserID string
}

func NewGalleryConfig() *GalleryConfig {
	g := new(GalleryConfig)
	g.conf = conf.Config.Gallery
	g.Type = g.conf["Type"]
	g.UserID = g.conf["UserID"]
	return g
}

func (g *GalleryConfig) Check() error {
	if len(g.conf) == 0 {
		return errors.New("Missing \"Gallery\" config.")
	}
	_, ok := g.conf["Type"]
	if !ok {
		return errors.New("Missing \"Type\" in gallery config.")
	}
	_, ok = g.conf["UserID"]
	if !ok {
		return errors.New("Missing \"UserID\" in gallery config.")
	}
	return nil
}

func (g *GalleryConfig) String() string {
	s := `"Gallery": {` + "\n"
	for key, val := range conf.Config.Gallery {
		s += fmt.Sprintf("    \"%s\": \"%s\",\n", key, val)
	}
	s = s[:len(s)-2] + "\n}\n"
	return s
}

func (g *GalleryConfig) SourceLink() string {
	if g.Type == "picasa" {
		return fmt.Sprintf("https://picasaweb.google.com/%s", g.UserID)
	}
	return ""
}

func missingConfig(ctx *web.Context, err error) string {
	gc := NewGalleryConfig()
	return adminBase.Render("gallery/missing-config.mustache", dict{
		"GalleryConfig": gc, "Error": err.Error()})
}

func galleryList(ctx *web.Context) string {
	//if app.RequireAuthentication(ctx) {
	//	return ""
	//}
	gc := NewGalleryConfig()
	err := gc.Check()
	if err != nil {
		return missingConfig(ctx, err)
	}

	return adminBase.Render("gallery/index.mustache", dict{
		"GalleryConfig": gc,
	})
}

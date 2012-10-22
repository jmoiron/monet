package gallery

import (
	"fmt"
	"github.com/hoisie/web"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/template"
	"time"
)

type M map[string]interface{}

var (
	adminBase     = template.Base{Path: "admin/base.mustache"}
	listPageSize  = 20
	indexListSize = 6
)

func auth(f func(*web.Context) string) func(*web.Context) string {
	ret := func(ctx *web.Context) string {
		if app.RequireAuthentication(ctx) {
			return ""
		}
		return f(ctx)
	}
	return ret
}

func AttachAdmin(url string) {
	web.Get(url+"gallery/", auth(galleryList))
	web.Post(url+"gallery/update-settings/", auth(updateSettings))
	web.Get(url+"gallery/force-update/", auth(forceUpdate))
}

func galleryList(ctx *web.Context) string {
	gc := LoadGalleryConfig()
	err := gc.Check()
	if err != nil {
		return missingConfig(ctx, err)
	}

	return adminBase.Render("gallery/index.mustache", M{
		"GalleryConfig": gc,
	})
}

func forceUpdate(ctx *web.Context) string {
	gc := LoadGalleryConfig()
	err := gc.Check()
	if err != nil {
		return missingConfig(ctx, err)
	}
	api := NewPicasaAPI(gc.UserID)
	albums, err := api.ListAlbums()
	if err != nil {
		return fmt.Sprintf("Error: %s", err.Error())
	}
	for _, album := range albums {
		db.Upsert(album)
	}
	gc.LastRun = time.Now().Unix()
	ctx.Redirect(302, "/admin/")
	return ""
}

func updateSettings(ctx *web.Context) string {
	gc := LoadGalleryConfig()
	gc.UserID = ctx.Params["userid"]
	gc.Type = ctx.Params["type"]
	gc.Save()
	ctx.Redirect(302, "/admin/")
	return ""
}

func missingConfig(ctx *web.Context, err error) string {
	gc := LoadGalleryConfig()
	return adminBase.Render("gallery/missing-config.mustache", M{
		"GalleryConfig": gc, "Error": err.Error()})
}

// *** Panels ***

type GalleryPanel struct{}

func (gp *GalleryPanel) Render() string {
	gc := LoadGalleryConfig()
	err := gc.Check()
	if err != nil {
		return template.Render("gallery/admin/gallery-panel-badconfig.mustache", M{
			"GalleryConfig": gc,
			"Error":         err.Error(),
		})
	}

	var albums []PicasaAlbum
	db.Find(&PicasaAlbum{}, nil).Limit(indexListSize).All(&albums)
	return template.Render("gallery/admin/gallery-panel.mustache", M{
		"GalleryConfig": gc,
		"Albums":        albums,
		"HasRun":        gc.LastRun > 0,
	})
}

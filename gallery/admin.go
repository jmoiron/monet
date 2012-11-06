package gallery

import (
	"fmt"
	"github.com/hoisie/web"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/template"
)

type M map[string]interface{}

var (
	adminBase     = template.Base{Path: "admin/base.mandira"}
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
	web.Get(url+"gallery/", auth(listAlbums))
	web.Post(url+"gallery/update-settings/", auth(updateSettings))
	web.Get(url+"gallery/force-update/", auth(forceUpdate))
	web.Get(url+"gallery/album/(.*)", listPhotos)
}

func listAlbums(ctx *web.Context) string {
	gc := LoadGalleryConfig()
	err := gc.Check()
	if err != nil {
		return missingConfig(ctx, err)
	}

	var albums []*PicasaAlbum
	db.Find(&PicasaAlbum{}, nil).All(&albums)

	return adminBase.Render("gallery/admin/index.mandira", M{
		"GalleryConfig": gc,
		"Albums":        albums,
	})
}

func listPhotos(ctx *web.Context, slug string) string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	album := new(PicasaAlbum)
	err := db.Find(album, M{"slug": slug}).One(album)
	if err != nil {
		println(err)
		println("Cannot find album " + slug)
		ctx.Redirect(302, "/admin/gallery/")
		return ""
	}
	var photos []*PicasaPhoto
	err = db.Find(&PicasaPhoto{}, M{"albumid": album.AlbumId}).
		Sort("position").All(&photos)
	if err != nil {
		println(err)
	}

	return adminBase.Render("gallery/admin/photo-list.mandira", M{
		"Album":  album,
		"Photos": photos,
	})
}

func forceUpdate(ctx *web.Context) string {
	gc := LoadGalleryConfig()
	err := gc.Check()
	if err != nil {
		return missingConfig(ctx, err)
	}
	api := NewPicasaAPI(gc.UserId)
	err = api.UpdateAll()
	if err != nil {
		fmt.Println(err)
	}
	if len(ctx.Params["from"]) > 0 {
		ctx.Redirect(302, ctx.Params["from"])
	} else {
		ctx.Redirect(302, "/admin/")
	}
	return ""
}

func updateSettings(ctx *web.Context) string {
	gc := LoadGalleryConfig()
	gc.UserId = ctx.Params["userid"]
	gc.Type = ctx.Params["type"]
	gc.Save()
	ctx.Redirect(302, "/admin/")
	return ""
}

func missingConfig(ctx *web.Context, err error) string {
	gc := LoadGalleryConfig()
	return adminBase.Render("gallery/missing-config.mandira", M{
		"GalleryConfig": gc, "Error": err.Error()})
}

// *** Panels ***

type GalleryPanel struct{}

func (gp *GalleryPanel) Render() string {
	gc := LoadGalleryConfig()
	err := gc.Check()
	if err != nil {
		return template.Render("gallery/admin/gallery-panel-badconfig.mandira", M{
			"GalleryConfig": gc,
			"Error":         err.Error(),
		})
	}

	var albums []PicasaAlbum
	db.Find(&PicasaAlbum{}, nil).Limit(indexListSize).All(&albums)
	return template.Render("gallery/admin/gallery-panel.mandira", M{
		"GalleryConfig": gc,
		"Albums":        albums,
		"HasRun":        gc.LastRun > 0,
	})
}

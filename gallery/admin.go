package gallery

import (
	"github.com/hoisie/web"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/template"
)

type M map[string]interface{}

var (
	adminBase     = template.Base{Path: "admin/base.mustache"}
	listPageSize  = 20
	indexListSize = 6
)

func AttachAdmin(url string) {
	web.Get(url+"gallery/", galleryList)
}

func galleryList(ctx *web.Context) string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	gc := NewGalleryConfig()
	err := gc.Check()
	if err != nil {
		return missingConfig(ctx, err)
	}

	return adminBase.Render("gallery/index.mustache", M{
		"GalleryConfig": gc,
	})
}

func missingConfig(ctx *web.Context, err error) string {
	gc := NewGalleryConfig()
	return adminBase.Render("gallery/missing-config.mustache", M{
		"GalleryConfig": gc, "Error": err.Error()})
}

// *** Panels ***

type GalleryPanel struct{}

func (gp *GalleryPanel) Render() string {
	gc := NewGalleryConfig()
	err := gc.Check()
	if err != nil {
		return template.Render("gallery/admin/gallery-panel-badconfig.mustache", M{
			"GalleryConfig": gc,
			"Error":         err.Error(),
		})
	}
	var albums []PicasaAlbum
	db.Find(&PicasaAlbum{}, nil).Limit(indexListSize).All(&albums)
	for _, a := range albums {
		db.Upsert(&a)
	}
	return template.Render("gallery/admin/gallery-panel.mustache", M{
		"GalleryConfig": gc,
		"Albums":        albums,
	})
}

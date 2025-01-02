package gallery

import (
	"errors"
	"fmt"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"labix.org/v2/mgo/bson"
)

var galleryConfig = new(GalleryConfig)
var loaded = false

type GalleryConfig struct {
	Id      bson.ObjectId `bson:"_id,omitempty"`
	Type    string
	UserId  string
	LastRun int64
}

func (g *GalleryConfig) Collection() string  { return "gallery" }
func (g *GalleryConfig) Indexes() [][]string { return [][]string{} }

func LoadGalleryConfig() *GalleryConfig {
	if loaded {
		return galleryConfig
	}
	db.Find(galleryConfig, nil).One(&galleryConfig)
	if len(galleryConfig.Type) == 0 {
		galleryConfig.Type = "picasa"
	}
	loaded = true
	return galleryConfig
}

func (g *GalleryConfig) Save() {
	if len(g.Id) == 0 {
		g.Id = bson.NewObjectId()
	}
	db.Cursor(galleryConfig).Upsert(bson.M{"_id": g.Id}, g)
	loaded = false
}

func (g *GalleryConfig) Check() error {
	if g.Type != "picasa" {
		return errors.New("Only \"picasa\" supported as gallery type.")
	}
	if len(g.UserId) == 0 {
		return errors.New("Missing \"UserId\" in gallery config.")
	}
	return nil
}

func (g *GalleryConfig) String() string {
	return fmt.Sprintf("GalleryConfig %s", app.PrettyPrint(*g))
}

func (g *GalleryConfig) SourceLink() string {
	if g.Type == "picasa" {
		return fmt.Sprintf("https://picasaweb.google.com/%s", g.UserId)
	}
	return ""
}

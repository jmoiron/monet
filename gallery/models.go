package gallery

import (
	"fmt"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"labix.org/v2/mgo/bson"
)

type SpiderData struct {
	Id      bson.ObjectId `bson:"_id,omitempty"`
	LastRun uint64
}

type PicasaAlbum struct {
	Id                 bson.ObjectId `bson:"_id,omitempty"`
	ApiUrl             string        // id
	AlbumId            string        //gphoto$id
	Published          string
	PublishedTimestamp uint64
	Updated            string
	UpdatedTimestamp   uint64
	Title              string
	Slug               string
	Summary            string
	Rights             string //gphoto$access
	Links              []string
	//Authors   []map[string]string // [{name:uri}, ..]
	NumPhotos int
	AlbumType string // gphoto$albumType
	Thumbnail string // media$thumbnail
}

func (a *PicasaAlbum) Collection() string { return "albums" }
func (a *PicasaAlbum) Indexes() [][]string {
	return [][]string{
		[]string{"albumid"},
		[]string{"title"},
	}
}
func (a *PicasaAlbum) Unique() bson.M {
	return bson.M{"albumid": a.AlbumId}
}
func (a *PicasaAlbum) PreSave() {
	// TODO: parse timestamps for UpdatedTimestamp & PublishedTimestamp
	if len(a.Slug) == 0 {
		a.Slug = app.Slugify(a.Title)
	}
	// TODO: slugify title
}

func ByAlbumId(id string) (*PicasaAlbum, error) {
	Album := new(PicasaAlbum)
	err := db.Find(Album, bson.M{"albumid": id}).One(Album)
	if err != nil {
		return Album, err
	}
	return Album, nil
}

func (p *PicasaAlbum) String() string {
	return fmt.Sprintf("Album %s (id#%s, %d photos)", p.Title, p.Id, p.NumPhotos)
}

func init() {
	db.Register(&PicasaAlbum{})
}

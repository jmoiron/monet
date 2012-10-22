package gallery

import (
	"fmt"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"labix.org/v2/mgo/bson"
)

type PicasaAlbum struct {
	Id                 bson.ObjectId `bson:"_id,omitempty"`
	ApiUrl             string        // id
	AlbumId            string        //gphoto$id
	Published          string
	PublishedTimestamp int64
	Updated            string
	UpdatedTimestamp   int64
	Title              string
	Slug               string
	Summary            string
	Rights             string //gphoto$access
	Links              []string
	AlbumType          string // gphoto$albumType
	Thumbnail          string // media$thumbnail
	NumPhotos          int
	//Authors   []map[string]string // [{name:uri}, ..]
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
	// parse timestamps for UpdatedTimestamp & PublishedTimestamp
	a.UpdatedTimestamp = ParsePicasaTime(a.Updated)
	a.PublishedTimestamp = ParsePicasaTime(a.Published)
	if len(a.Slug) == 0 {
		a.Slug = app.Slugify(a.Title)
	}
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

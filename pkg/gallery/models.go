package gallery

import (
	"fmt"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"labix.org/v2/mgo/bson"
)

type Image struct {
	Url    string
	Height int
	Width  int
}

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
	Enabled            bool `bson:",omitempty"`
	//Authors   []map[string]string // [{name:uri}, ..]
}

type PicasaPhoto struct {
	Id                 bson.ObjectId `bson:"_id,omitempty"`
	ApiUrl             string        // id
	AlbumId            string        //gphoto$albumid
	PhotoId            string        // gphoto$id
	Published          string
	PublishedTimestamp int64
	Updated            string
	UpdatedTimestamp   int64
	Title              string
	Slug               string
	Summary            string
	Rights             string //gphoto$access
	Links              []string
	Size               int
	Height             int
	Width              int
	ExifTags           map[string]string
	Position           int
	Large              Image
	Thumbnails         []Image
	Enabled            bool `bson:",omitempty"`
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

func (a *PicasaAlbum) Disable() {
	cur := db.Cursor(a)
	cur.Update(bson.M{"_id": a.Id}, bson.M{"enabled": false})
}

func (a *PicasaAlbum) Enable() {
	cur := db.Cursor(a)
	cur.Update(bson.M{"_id": a.Id}, bson.M{"enabled": true})
}

func (a *PicasaAlbum) AlbumLink() string {
	if len(a.Links) > 2 {
		return a.Links[1]
	}
	return ""
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

func (p *PicasaPhoto) Collection() string { return "photos" }
func (p *PicasaPhoto) Indexes() [][]string {
	return [][]string{
		[]string{"photoid"},
		[]string{"albumid", "published"},
		[]string{"albumid", "position"},
	}
}
func (p *PicasaPhoto) Unique() bson.M {
	return bson.M{"albumid": p.AlbumId, "photoid": p.PhotoId}
}
func (p *PicasaPhoto) PreSave() {
	// parse timestamps for UpdatedTimestamp & PublishedTimestamp
	p.UpdatedTimestamp = ParsePicasaTime(p.Updated)
	p.PublishedTimestamp = ParsePicasaTime(p.Published)
	if len(p.Slug) == 0 {
		p.Slug = app.Slugify(p.Title)
	}
}
func (p *PicasaPhoto) Disable() {
	cur := db.Cursor(p)
	cur.Update(bson.M{"_id": p.Id}, bson.M{"enabled": false})
}

func (p *PicasaPhoto) Enable() {
	cur := db.Cursor(p)
	cur.Update(bson.M{"_id": p.Id}, bson.M{"enabled": true})
}

func (p *PicasaPhoto) MediumThumbnail() Image {
	if len(p.Thumbnails) == 3 {
		return p.Thumbnails[1]
	}
	return p.Thumbnails[len(p.Thumbnails)-1]
}

func (p *PicasaPhoto) FullSize() string {
	// FIXME: Eventually, return a URL which represents the largest available
	// size on the other server, and for picasa that is a dimension we cannot
	// request in the original list
	return p.Large.Url
}

func init() {
	db.Register(&PicasaAlbum{})
	db.Register(&PicasaPhoto{})
}

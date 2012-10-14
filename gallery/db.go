package gallery

import (
	"fmt"
	"github.com/jmoiron/monet/db"
	"labix.org/v2/mgo/bson"
)

type PicasaAlbum struct {
	Id        bson.ObjectId "_id"
	ApiUrl    string        // id
	AlbumId   string        //gphoto$id
	Published string
	Updated   string
	Title     string
	Summary   string
	Rights    string //gphoto$access
	Links     []string
	//Authors   []map[string]string // [{name:uri}, ..]
	NumPhotos int
	AlbumType string // gphoto$albumType
	Thumbnail string // media$thumbnail
}

type AlbumCursor struct{ db.Cursor }

func Albums() *AlbumCursor {
	a := new(AlbumCursor)
	a.C = db.Db.C("albums")
	return a
}

func (a *AlbumCursor) ByAlbumId(id string) (*PicasaAlbum, error) {
	Album := new(PicasaAlbum)
	err := a.C.Find(bson.M{"albumid": id}).One(Album)
	if err != nil {
		return Album, err
	}
	return Album, nil
}

func (p *PicasaAlbum) Update() error {
	if len(p.Id) == 0 {
		p.Id = bson.NewObjectId()
	}
	_, err := Albums().C.Upsert(bson.M{"albumid": p.AlbumId}, &p)
	return err
}

func (p *PicasaAlbum) String() string {
	return fmt.Sprintf("Album %s (id#%s, %d photos)", p.Title, p.Id, p.NumPhotos)
}

func InitCollection() {
	db.Db.C("albums").EnsureIndexKey("albumid")
}

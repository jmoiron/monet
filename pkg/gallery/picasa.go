package gallery

import (
	"encoding/json"
	"fmt"
	"github.com/jmoiron/jsonq"
	"github.com/jmoiron/monet/db"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"
	"time"
)

type WaitGroupErr struct {
	sync.WaitGroup
	Errors []error
}

func (w *WaitGroupErr) Error(err error) {
	w.Errors = append(w.Errors, err)
}

func (w *WaitGroupErr) GetError() error {
	if len(w.Errors) > 0 {
		return w.Errors[0]
	}
	return nil
}

// join multiple url bits into one
func UrlJoin(strs ...string) string {
	ss := make([]string, len(strs))
	for i, s := range strs {
		if i == 0 {
			ss[i] = strings.TrimRight(s, "/")
		} else {
			ss[i] = strings.TrimLeft(s, "/")
		}
	}
	return strings.Join(ss, "/")
}

var Client = &http.Client{
	// keep user-agent:
	// https://groups.google.com/forum/?fromgroups#!topic/golang-nuts/OwGvopYXpwE%5B1-25%5D	
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		old := via[0]
		req.Header.Set("User-Agent", old.UserAgent())
		return nil
	},
}

func HttpGet(url string) (string, error) {
	var body []byte
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return string(body), err
	}
	req.Header.Set("GData-Version", "2")

	resp, err := Client.Do(req)
	if err != nil {
		return string(body), err
	}
	defer resp.Body.Close()
	body, err = ioutil.ReadAll(resp.Body)

	if err != nil {
		return string(body), err
	}
	return string(body), nil
}

// Given a URL, fetch it and return a string map of the data
func HttpGetJson(url string) (map[string]interface{}, error) {
	data := map[string]interface{}{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return data, err
	}
	req.Header.Set("GData-Version", "2")
	resp, err := Client.Do(req)
	if err != nil {
		return data, err
	}

	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&data)
	if err != nil {
		return data, err
	}
	return data, nil
}

// Taken a timestamp in the picasa format, which seems like the standard
// iso format time, return a int64 timestamp, ex:
//    2009-04-27T07:18:46.000Z -> 
func ParsePicasaTime(timestamp string) int64 {
	t, err := time.Parse("2006-01-02T15:04:05.000Z", timestamp)
	if err != nil {
		return 0
	}
	return t.Unix()
}

const (
	PicasaAPIBase = "https://picasaweb.google.com/data/feed/api/"
)

type PicasaAPI struct {
	UserId string
}

func NewPicasaAPI(UserId string) *PicasaAPI {
	p := new(PicasaAPI)
	p.UserId = UserId
	return p
}

// Update all picasa data and keep track of required bookkeeping
func (p *PicasaAPI) UpdateAll() error {
	gc := LoadGalleryConfig()
	err := gc.Check()
	if err != nil {
		return err
	}
	err = p.UpdateAlbums()
	if err != nil {
		return err
	}
	var albums []*PicasaAlbum
	db.Find(&PicasaAlbum{}, nil).All(&albums)

	var wg WaitGroupErr

	for _, album := range albums {
		wg.Add(1)
		go func(a *PicasaAlbum) {
			defer wg.Done()
			err := p.UpdatePhotos(a)
			if err != nil {
				wg.Error(err)
			}
		}(album)
	}

	wg.Wait()

	gc.LastRun = time.Now().Unix()
	gc.Save()

	err = wg.GetError()
	if err != nil {
		return err
	}
	return nil
}

// Update all picasa album info
func (p *PicasaAPI) UpdateAlbums() error {
	albums, err := p.ListAlbums()
	if err != nil {
		return err
	}
	for _, album := range albums {
		db.Upsert(album)
	}
	return nil
}

func (p *PicasaAPI) UpdatePhotos(album *PicasaAlbum) error {
	photos, err := p.ListPhotos(album)
	if err != nil {
		return err
	}
	for _, photo := range photos {
		db.Upsert(photo)
	}
	return nil
}

func (p *PicasaAPI) ListPhotos(album *PicasaAlbum) ([]*PicasaPhoto, error) {
	url := UrlJoin(PicasaAPIBase, "user", p.UserId, "albumid",
		fmt.Sprintf("%s?alt=json&imgmax=1600", album.AlbumId))
	println(url)
	js, err := HttpGetJson(url)
	if err != nil {
		return []*PicasaPhoto{}, err
	}
	photos, err := p.parsePhotos(js)
	if err != nil {
		return photos, err
	}
	return photos, nil
}

func (p *PicasaAPI) ListAlbums() ([]*PicasaAlbum, error) {
	url := UrlJoin(PicasaAPIBase, fmt.Sprintf("/user/%s?alt=json", p.UserId))
	println(url)
	js, err := HttpGetJson(url)
	if err != nil {
		return []*PicasaAlbum{}, err
	}
	albums, err := p.parseAlbums(js)
	if err != nil {
		return albums, err
	}
	return albums, nil
}

func (p *PicasaAPI) parseAlbums(js interface{}) ([]*PicasaAlbum, error) {
	jq := jsonq.NewQuery(js)
	entries, _ := jq.Array("feed", "entry")
	albums := make([]*PicasaAlbum, len(entries))

	for i, entry := range entries {
		eq := jsonq.NewQuery(entry)
		album := new(PicasaAlbum)
		album.ApiUrl, _ = eq.String("id", "$t")
		album.AlbumId, _ = eq.String("gphoto$id", "$t")
		album.Published, _ = eq.String("published", "$t")
		album.Updated, _ = eq.String("updated", "$t")
		album.Title, _ = eq.String("title", "$t")
		album.Summary, _ = eq.String("summary", "$t")
		album.Rights, _ = eq.String("gphoto$access", "$t")
		album.NumPhotos, _ = eq.Int("gphoto$numphotos", "$t")
		links, _ := eq.Array("link")
		for _, link := range links {
			lq := jsonq.NewQuery(link)
			s, err := lq.String("href")
			if err != nil {
				continue
			}
			album.Links = append(album.Links, s)
		}
		album.AlbumType, _ = eq.String("gphoto$albumType", "$t")
		album.Thumbnail, _ = eq.String("media$group", "media$thumbnail", "0", "url")
		albums[i] = album
	}
	return albums, nil
}

func (p *PicasaAPI) parsePhotos(js interface{}) ([]*PicasaPhoto, error) {
	jq := jsonq.NewQuery(js)
	entries, _ := jq.Array("feed", "entry")
	photos := make([]*PicasaPhoto, len(entries))

	for i, entry := range entries {
		eq := jsonq.NewQuery(entry)
		photo := new(PicasaPhoto)
		photo.ApiUrl, _ = eq.String("id", "$t")
		photo.PhotoId, _ = eq.String("gphoto$id", "$t")
		photo.AlbumId, _ = eq.String("gphoto$albumid", "$t")
		photo.Published, _ = eq.String("published", "$t")
		photo.Updated, _ = eq.String("updated", "$t")
		photo.Title, _ = eq.String("title", "$t")
		photo.Summary, _ = eq.String("summary", "$t")
		photo.Rights, _ = eq.String("gphoto$access", "$t")
		links, _ := eq.Array("link")
		for _, link := range links {
			lq := jsonq.NewQuery(link)
			s, err := lq.String("href")
			if err != nil {
				continue
			}
			photo.Links = append(photo.Links, s)
		}
		photo.Height, _ = eq.Int("gphoto$height", "$t")
		photo.Width, _ = eq.Int("gphoto$width", "$t")
		photo.Size, _ = eq.Int("gphoto$size", "$t")

		photo.Large.Url, _ = eq.String("media$group", "media$content", "0", "url")
		photo.Large.Height, _ = eq.Int("media$group", "media$content", "0", "height")
		photo.Large.Width, _ = eq.Int("media$group", "media$content", "0", "width")

		photo.Position, _ = eq.Int("gphoto$position", "$t")

		thumbnails, _ := eq.Array("media$group", "media$thumbnail")
		for _, thumb := range thumbnails {
			tq := jsonq.NewQuery(thumb)
			t := Image{}
			t.Url, _ = tq.String("url")
			t.Height, _ = tq.Int("height")
			t.Width, _ = tq.Int("width")
			photo.Thumbnails = append(photo.Thumbnails, t)
		}

		photo.ExifTags = map[string]string{}

		tags, _ := eq.Object("exif$tags")
		for key, obj := range tags {
			oq := jsonq.NewQuery(obj)
			photo.ExifTags[key], _ = oq.String("$t")
		}

		photos[i] = photo
	}
	return photos, nil
}

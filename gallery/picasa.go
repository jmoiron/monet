package gallery

import (
	"encoding/json"
	"fmt"
	"github.com/jmoiron/jsonq"
	"io/ioutil"
	"net/http"
	"strings"
)

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

const (
	PicasaAPIBase = "https://picasaweb.google.com/data/feed/api/"
)

type PicasaAPI struct {
	UserID string
}

func NewPicasaAPI(UserID string) *PicasaAPI {
	p := new(PicasaAPI)
	p.UserID = UserID
	return p
}

func (p *PicasaAPI) ListAlbums() ([]*PicasaAlbum, error) {
	url := UrlJoin(PicasaAPIBase, fmt.Sprintf("/user/%s?alt=json", p.UserID))
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

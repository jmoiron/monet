package gallery

import (
	"errors"
	"fmt"
	"github.com/jmoiron/monet/conf"
)

type GalleryConfig struct {
	conf   map[string]string
	Type   string
	UserID string
}

func NewGalleryConfig() *GalleryConfig {
	g := new(GalleryConfig)
	g.conf = conf.Config.Gallery
	g.Type = g.conf["Type"]
	g.UserID = g.conf["UserID"]
	return g
}

func (g *GalleryConfig) Check() error {
	if len(g.conf) == 0 {
		return errors.New("Missing \"Gallery\" config.")
	}
	_, ok := g.conf["Type"]
	if !ok {
		return errors.New("Missing \"Type\" in gallery config.")
	}
	_, ok = g.conf["UserID"]
	if !ok {
		return errors.New("Missing \"UserID\" in gallery config.")
	}
	return nil
}

func (g *GalleryConfig) String() string {
	s := `"Gallery": {` + "\n"
	for key, val := range conf.Config.Gallery {
		s += fmt.Sprintf("    \"%s\": \"%s\",\n", key, val)
	}
	s = s[:len(s)-2] + "\n}\n"
	return s
}

func (g *GalleryConfig) SourceLink() string {
	if g.Type == "picasa" {
		return fmt.Sprintf("https://picasaweb.google.com/%s", g.UserID)
	}
	return ""
}

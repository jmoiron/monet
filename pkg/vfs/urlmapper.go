package vfs

import (
	"fmt"
	"path/filepath"
)

// A URLMapper maps VFS names to URL routes.
//
// Eg. the "images" filesystem could map to "/static/img/". This allows
// code to create canonical URLs while allowing the underlying filesystem
// to be re-arranged.
type URLMapper interface {
	// GetPrefix returns the URL prefix for a filesystem name, or an error if
	// there isn't any prefix registered for name.
	GetPrefix(name string) (string, error)

	// GetURL returns the URL for a path under the filesystem name, essentially
	// combining it with the prefix for name in one step. If there is no
	// filesystem registered for name, an error is returned.
	GetURL(name, path string) (string, error)

	// GetMap returns a copy of the internal URL map, eg. for iterating over.
	GetMap() map[string]string
}

// NewURLMapper returns a new URLMapper for the given map of names to prefixes.
func NewURLMapper(mapping map[string]string) URLMapper {
	return &urlMapper{urlMap: mapping}
}

// A urlMapper maps filesystems to URLs, so eg. the "images" filesystem
// could be mapped to a route like "/static/img/". The mapper allows code
// to create canonical URLs independent of their physical location.
type urlMapper struct {
	urlMap map[string]string
}

func (u *urlMapper) GetPrefix(name string) (string, error) {
	if prefix, ok := u.urlMap[name]; ok {
		return prefix, nil
	}
	return "", fmt.Errorf("no URLs mapped for fs '%s'", name)
}

// GetURL gets a URL for the path anchored at name.
func (u *urlMapper) GetURL(name, path string) (string, error) {
	prefix, err := u.GetPrefix(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(prefix, path), nil
}

func (u *urlMapper) GetMap() map[string]string {
	// return a copy
	m := make(map[string]string, len(u.urlMap))
	for k, v := range u.urlMap {
		m[k] = v
	}
	return m
}

var _ URLMapper = &urlMapper{}

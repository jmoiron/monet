package hotswap

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// An FSS is a system of filesystems. Paths can be registered and then
// retrieved by name. It's a trace buster buster for filesystems.
type FSS interface {
	AddPath(name, path string) error
	Add(name string, x fs.FS) error
	Get(name string) (fs.FS, error)
	GetPath(name string) (string, error)
	Remove(name string) error
}

// A URLMapper is an FSS that can also look up URLs for files within
// the FSS.
type URLMapper interface {
	FSS
	GetURL(name, path string) (string, error)
	All() map[string]string
}

type fss struct {
	mu    sync.RWMutex
	fss   map[string]fs.FS
	paths map[string]string
}

func NewFSS() FSS {
	return &fss{fss: make(map[string]fs.FS), paths: make(map[string]string)}
}

// AddPath adds the directory at path to f using name. Its path will be resolvable
// via GetPath(name).
func (f *fss) AddPath(name, path string) error {
	xstat, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !xstat.IsDir() {
		return fmt.Errorf("AddPath: path '%s' is not a directory", path)
	}
	if err := f.addPath(name, path); err != nil {
		return err
	}
	return f.Add(name, os.DirFS(path))
}

func (f *fss) addPath(name, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.paths[name]; ok {
		return fmt.Errorf("AddPath: path with name '%s' already exists", name)
	}
	f.paths[name] = path
	return nil
}

func (f *fss) GetPath(name string) (string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	path, ok := f.paths[name]
	if !ok {
		return "", fmt.Errorf("no path with name %s", name)
	}
	return path, nil
}

// Add x to f under name. When added in this way, the fs will not have
// a path. This might be intentional (eg. if using virtual filesystems).
func (f *fss) Add(name string, x fs.FS) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.fss[name]; ok {
		return fmt.Errorf("Add: fs with name '%s' already exisits", name)
	}
	f.fss[name] = x
	return nil
}

func (f *fss) Get(name string) (fs.FS, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	x, ok := f.fss[name]
	if !ok {
		return nil, fmt.Errorf("Get: fs with name '%s' not found", name)
	}
	return x, nil
}

func (f *fss) Remove(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.fss[name]; !ok {
		return fmt.Errorf("Remove: fs with name '%s' not found", name)
	}
	return nil
}

// A URLMapper is an FSS that can also map paths to urls
type urlMapper struct {
	// TODO: this isn't safe to edit once it's created, maybe it should be?
	FSS
	m map[string]string
}

func NewURLMapper(URLMap map[string]string) URLMapper {
	return &urlMapper{
		FSS: NewFSS(),
		m:   URLMap,
	}
}

func (m *urlMapper) GetURL(name, path string) (string, error) {
	if _, err := m.Get(name); err != nil {
		return "", err
	}
	s, ok := m.m[name]
	if !ok {
		return "", fmt.Errorf("no URLs mapped for fs '%s'", name)
	}
	return filepath.Join(s, path), nil
}

// All returns the mappings of each FS with their intended URL prefix.
func (m *urlMapper) All() map[string]string {
	// don't expose the internal mapping directly
	result := make(map[string]string)
	for name, path := range m.m {
		result[name] = path
	}
	return result
}

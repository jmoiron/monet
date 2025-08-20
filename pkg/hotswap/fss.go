package hotswap

import (
	"fmt"
	"io/fs"
	"os"
	"sync"
)

// An FSS is a system of filesystems. Paths can be registered and then
// retrieved by name. It's a trace buster buster for filesystems.
type FSS interface {
	AddPath(name, path string) error
	Add(name string, x fs.FS) error
	Get(name string) (fs.FS, error)
	Remove(name string) error
}

// A URLMapper is an FSS that can also look up URLs for files within
// the FSS.
type URLMapper interface {
	FSS
	GetURL(name, path string) (string, error)
}

type fss struct {
	mu  sync.RWMutex
	fss map[string]fs.FS
}

func NewFSS() FSS {
	return &fss{fss: make(map[string]fs.FS)}
}

func (f *fss) AddPath(name, path string) error {
	xstat, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !xstat.IsDir() {
		return fmt.Errorf("AddPath: path '%s' is not a directory", path)
	}
	return f.Add(name, os.DirFS(path))
}

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
	FSS
	m map[string]string
}

func NewURLMapper(URLMap map[string]string) URLMapper {
	return &urlMapper{
		FSS: NewFSS(),
		m:   make(map[string]string),
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
	return s, nil
}

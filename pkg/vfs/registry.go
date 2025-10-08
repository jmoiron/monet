package vfs

import (
	"fmt"
	"io/fs"
	"os"
	"sync"
)

// A Registry is a collection of "named" filesystems, where a filesystem is
// simply some kind of directory system in the form of an fs.FS. These
// filesystems can be virtual or be anchored at a path on the "real" filesystem.

// Registry is a system of filesystems that can map paths to URLs.
// Paths can be registered and then retrieved by name.
type Registry interface {
	// Filesystem operations
	AddPath(name, path string) error
	Add(name string, x fs.FS) error
	Get(name string) (fs.FS, error)
	GetPath(name string) (string, error)
	Remove(name string) error

	Mapper() URLMapper

	// Uploader creates an uploader for the named filesystem
	CreateUploader(name string) (*Uploader, error)
}

type registry struct {
	mu  sync.RWMutex
	fss map[string]fs.FS
	m   URLMapper
}

// NewRegistry creates a new VFS registry with the provided URL mapper. If the
// mapper is nil, then url routing will not be available.
func NewRegistry(m URLMapper) Registry {
	return &registry{
		fss: make(map[string]fs.FS),
		m:   m,
	}
}

func (r *registry) Mapper() URLMapper { return r.m }

// AddPath adds the directory at path to the registry using name.
// Its path will be resolvable via GetPath(name).
func (r *registry) AddPath(name, path string) error {
	xstat, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !xstat.IsDir() {
		return fmt.Errorf("AddPath: path '%s' is not a directory", path)
	}
	return r.Add(name, newPathFS(path))
}

// GetPath returns the filesystem path for the given name
func (r *registry) GetPath(name string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fs, ok := r.fss[name]
	if !ok {
		return "", fmt.Errorf("no fs with name %s", name)
	}
	if p, ok := fs.(PathFS); ok {
		return p.Path(), nil
	}
	return "", fmt.Errorf("no path for fs with name %s", name)
}

// Add x to the registry under name. When added in this way, the fs will not have
// a path. This might be intentional (eg. if using virtual filesystems).
func (r *registry) Add(name string, x fs.FS) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.fss[name]; ok {
		return fmt.Errorf("Add: fs with name '%s' already exists", name)
	}
	r.fss[name] = x
	return nil
}

// Get returns the filesystem for the given name
func (r *registry) Get(name string) (fs.FS, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	x, ok := r.fss[name]
	if !ok {
		return nil, fmt.Errorf("Get: fs with name '%s' not found", name)
	}
	return x, nil
}

// Remove removes the filesystem with the given name
func (r *registry) Remove(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.fss[name]; !ok {
		return fmt.Errorf("Remove: fs with name '%s' not found", name)
	}
	delete(r.fss, name)
	return nil
}

// CreateUploader creates an uploader for the named filesystem
func (r *registry) CreateUploader(name string) (*Uploader, error) {
	fs, err := r.Get(name)
	if err != nil {
		return nil, fmt.Errorf("CreateUploader: %w", err)
	}

	if r.m == nil {
		return nil, fmt.Errorf("CreateUploader: no URL mapper available")
	}

	urlPrefix, err := r.m.GetURL(name, "")
	if err != nil {
		return nil, fmt.Errorf("CreateUploader: failed to get URL prefix: %w", err)
	}

	return NewUploader(fs, urlPrefix)
}

// A PathFS is an FS that corresponds to a particular path.
type PathFS interface {
	fs.FS
	Path() string
}

type pathFS struct {
	fs.FS
	path string
}

func (p *pathFS) Path() string {
	return p.path
}

func newPathFS(path string) PathFS {
	return &pathFS{FS: os.DirFS(path), path: path}
}

func newPathFSWithFS(path string, f fs.FS) PathFS {
	return &pathFS{FS: f, path: path}
}
